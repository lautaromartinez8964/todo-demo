package main

import (
	"context" // 用于数据库和Redis操作的超时控制
	"encoding/json"
	"errors"
	"net/http"

	// 用于GORM结构体在写入Redis时的JSON序列化
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/locales/zh"              //中文CLDR语料库
	ut "github.com/go-playground/universal-translator" // 翻译器管理器
	"github.com/go-playground/validator/v10"
	zh_translations "github.com/go-playground/validator/v10/translations/zh"

	"github.com/redis/go-redis/v9"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// =====
// 1.全局系统架构：多连接池与高并发解耦Channel
// =====

var DB *gorm.DB         // 全局MySQL连接池指针
var RDB *redis.Client   // 全局Redis缓存连接池指针
var trans ut.Translator // 全局翻译器

// 🌟 核心亮点：高并发异步任务派发通道（Day 9 生产者-消费者传送带）
// 缓冲区设置为100，允许在极高并发下堆积100个推理任务，超过100后生产者会阻塞等待
var TaskQueue = make(chan uint, 100) // 传输的是任务自增主键ID
var WG sync.WaitGroup                // 主线程退出时，优雅等待所有后台检测goroutine退出

// =====
// 2.数据库模型定义(Entities) & 安全DTO定义
// =====

// 物理表实体
type Task struct {
	gorm.Model
	TaskName  string  `gorm:"type:varchar(255);not null;index"`
	TaskType  string  `gorm:"type:varchar(255);not null"`
	Latitude  float64 `gorm:"not null"`
	Longitude float64 `gorm:"not null"`
	Status    bool    `gorm:"default:false"`
}

// 安全输入模型（杜绝批量赋值篡改漏洞)
type TaskCreateInput struct {
	TaskName  string  `json:"task_name" binding:"required,min=3"`
	TaskType  string  `json:"task_type" binding:"required"`
	Latitude  float64 `json:"latitude" binding:"required,chinalat"`
	Longitude float64 `json:"longitude" binding:"required,chinalng"`
}

// =====
// 3.自定义多层校验器（GIS空间约束）
// =====

func validateChinaLat(f1 validator.FieldLevel) bool {
	lat := f1.Field().Float()
	return lat >= 3.86 && lat <= 53.55
}

func validateChinaLng(f1 validator.FieldLevel) bool {
	lng := f1.Field().Float()
	return lng >= 73.66 && lng <= 135.05
}

// 跨字段结构体校验：禁飞区控制
// 参数s1是validator传入的结构体级别校验上下文，通过它可以访问整个结构体实例
func validateNoFly(s1 validator.StructLevel) {
	input := s1.Current().Interface().(TaskCreateInput)
	// 如果经纬度落在禁飞区，不给飞
	if input.Latitude >= 39.00 && input.Latitude <= 41.00 && input.Longitude >= 115.00 && input.Longitude <= 117.00 {
		s1.ReportError(input.TaskName, "task_name", "TaskName", "nofly", "")
	}
}

// =====
// 4.核心系统初始化模态
// =====

func initDatabases() {
	// A.连接MySQL
	dsn := "root:your_password_123456@tcp(127.0.0.1:3306)/todo_db?charset=utf8mb4&parseTime=True&loc=Local"
	var err error
	DB, err = gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		panic("CRITICAL:MySQL连接失败! + err.Error()")
	}
	DB.AutoMigrate(&Task{})
	fmt.Println("🚀 [MySQL] 自动建表与表结构同步顺利打通！")

	// B.连接Redis(Day8)
	RDB = redis.NewClient(&redis.Options{
		Addr: "127.0.0.1:6379",
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := RDB.Ping(ctx).Err(); err != nil {
		panic("CRITICAL:Redis连接失败!" + err.Error())
	}
	fmt.Println("🚀 [Redis] 内存引擎 Ping 握手通畅！")
}

func initValidator() {
	if v, ok := binding.Validator.Engine().(*validator.Validate); ok {
		// 1.让报错的变量名能自动显示为前端看懂的json字段名
		v.RegisterTagNameFunc(func(fld reflect.StructField) string {
			name := strings.SplitN(fld.Tag.Get("json"), ",", 2)[0]
			if name == "-" {
				return ""
			}
			return name
		})

		// 2.加载翻译字典
		zhLocale := zh.New()
		uni := ut.New(zhLocale, zhLocale)
		trans, _ = uni.GetTranslator("zh")
		zh_translations.RegisterDefaultTranslations(v, trans)

		// 3.注册自定义地理空间约束
		v.RegisterValidation("chinalat", validateChinaLat)
		v.RegisterTranslation("chinalat", trans, func(ut ut.Translator) error {
			return ut.Add("chinalat", "{0}超出中国有效纬度范围!", true)
		}, func(ut ut.Translator, fe validator.FieldError) string {
			t, _ := ut.T("chinalat", fe.Field())
			return t
		})

		v.RegisterValidation("chinalng", validateChinaLng)
		v.RegisterTranslation("chinalng", trans, func(ut ut.Translator) error {
			return ut.Add("chinalng", "{0}超出中国有效经度范围!", true)
		}, func(ut ut.Translator, fe validator.FieldError) string {
			t, _ := ut.T("chinalng", fe.Field())
			return t
		})

		// 4.注册结构体关联禁飞区校验
		v.RegisterStructValidation(validateNoFly, TaskCreateInput{})

		fmt.Println("🚀 [I18n] 自定义空间校验器与全自动翻译器部署就绪！")
	}
}

// =====
// 5.高并发核心:后台异步推理消费者协程(Goroutine Worker)
// =====

// startBackgroundTaskWorker默默在后台盯着TaskQueue这个传送带
// 一代发现有任务ID送过来，立刻叫醒，模拟调用YOLO推理检测，并回写MySQL
func startBackgroundTaskWorker(ctx context.Context) {
	defer WG.Done() // goroutine退出的时候，通知WaitGroup计数器-1
	fmt.Println("[Worker]后端异步遥感推理goroutine已就位,开始紧盯流水线...")

	for {
		select {
		case <-ctx.Done():
			// 如果主线程（main) 发出了关闭指令(cancel), 优雅退出
			fmt.Println("👷 [Worker] 收到主线程安全退出信号，正在优雅关闭协程并妥善保存现场...")
			return
		case taskID, ok := <-TaskQueue:
			if !ok {
				// 如果管道被关闭了，说明不会有任务了，安全下班
				return
			}
			// 真正处理这个任务
			processTask(taskID)
		}
	}
}

// 模拟调用Python推理并回写
func processTask(taskID uint) {
	fmt.Printf("\n[Worker]发现新任务ID:%d, 开始调用YOLO算法服务识别...\n", taskID)

	// 模拟复杂的算法识别过程
	time.Sleep(2 * time.Second)

	var taskProcess Task
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := DB.WithContext(ctx).First(&taskProcess, taskID).Error
	if err != nil {
		fmt.Println("[Worker]在数据库里没有找到这个任务呢")
		return
	}
	result := DB.WithContext(ctx).Model(&taskProcess).Select("status").Updates(map[string]any{
		"status": true,
	})
	if result.Error != nil {
		fmt.Println("[Worker]更新任务状态失败！")
		return
	}

	// redis缓存一致性：因为任务状态发生了修改，必须立刻删除该任务的缓存
	cacheKey := fmt.Sprintf("tasks:detail:%d", taskProcess.ID)
	err = RDB.Del(ctx, cacheKey).Err()
	if err != nil {
		fmt.Println("[Worker]更新缓存失败！")
	}
}

// =====
// 6.主程序与网络控制器层(Gin Web API Handlers)
// =====

func ma() {
	// A.初始化双引擎和验证器
	initDatabases()
	initValidator()

	// B.启动后台异步处理goroutine(生产者-消费者模型)
	ctx, cancelGlobal := context.WithCancel(context.Background())
	WG.Add(1)                         // 告诉WaitGroup我们要启动一个后台任务了
	go startBackgroundTaskWorker(ctx) // 启动消费者

	r := gin.Default()

	// -----
	// 🚀 [接口 1]：高并发异步提交任务接口 (POST /tasks)
	// -----
	r.POST("/tasks", func(c *gin.Context) {
		timeoutCtx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		c.Request = c.Request.WithContext(timeoutCtx)

		var input TaskCreateInput
		// A.校验器开始校验，JSON转结构体
		if err := c.ShouldBindBodyWithJSON(&input); err != nil {
			errs, ok := err.(validator.ValidationErrors)
			if !ok {
				c.JSON(http.StatusBadRequest, gin.H{
					"status": "error",
					"error":  "非法JSON",
				})
				return
			}
			c.JSON(http.StatusBadRequest, gin.H{
				"status": "error",
				"error":  errs.Translate(trans),
			})
			return
		}

		// B.过滤：将安全的DTO字段拷贝给实体
		newTask := Task{
			TaskName:  input.TaskName,
			TaskType:  input.TaskType,
			Latitude:  input.Latitude,
			Longitude: input.Longitude,
			Status:    false,
		}

		if err := DB.WithContext(timeoutCtx).Create(&newTask).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"status": "error",
				"error":  "MySQL创建任务失败:" + err.Error(),
			})
			return
		}

		// C.一致性保障：强制擦除可能存在ID详情的Redis焕春
		cacheKey := fmt.Sprintf("tasks:detail:%d", newTask.ID)
		RDB.Del(timeoutCtx, cacheKey)

		// 生产者核心：将新任务的ID，塞入异步处理通道
		// 后台的Worker协程会被立刻唤醒并进行处理
		TaskQueue <- newTask.ID

		// 快速返回200给用户，不卡死，不等待，用户体验完美
		c.JSON(http.StatusOK, gin.H{
			"status":  "success",
			"message": "遥感检测任务创建成功，已提交后台异步推理，请稍后查看结果!",
			"data":    newTask,
		})

	})

	// -----
	// [接口2]：高并发冷热分离任务详情查询(GET/tasks/:id)
	// -----
	r.GET("/tasks/:id", func(c *gin.Context) {
		idStr := c.Param("id")
		id, err := strconv.Atoi(idStr)
		if err != nil || id <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{
				"status": "error",
				"error":  "ID格式非法",
			})
			return
		}

		// claim高并发专用的超时context
		timeoutCtx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		c.Request = c.Request.WithContext(timeoutCtx)

		// redis键
		cacheKey := fmt.Sprintf("tasks:detail:%d", id)

		// A.热查询：先查redis
		cacheVal, err := RDB.Get(timeoutCtx, cacheKey).Result()

		// 缓存命中(Cache Hit)
		if err == nil {
			fmt.Println("[Cache Hit!]命中Redis缓存！")
			var cachedTask Task
			json.Unmarshal([]byte(cacheVal), &cachedTask) // 反序列化
			c.JSON(http.StatusOK, gin.H{
				"status": "success",
				"source": "redis_cache",
				"data":   cachedTask,
			})
			return
		}

		// 如果发生出了未找到(redis.Nil)外的报错，警告
		if !errors.Is(err, redis.Nil) {
			fmt.Printf("[Warning]Redis读取发生异常:%s\n", err.Error())
		}

		// B.冷查询：缓存未命中，查MySQL
		fmt.Println("[Cache Miss]缓存未命中，查询MySQL...")
		var dbTask Task
		dbErr := DB.WithContext(timeoutCtx).First(&dbTask, id).Error
		if dbErr != nil {
			if errors.Is(dbErr, gorm.ErrRecordNotFound) {
				c.JSON(http.StatusNotFound, gin.H{
					"status": "error",
					"error":  "任务不存在",
				})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{
				"status": "error",
				"error":  "查询MySQL错误",
			})
			return

		}

		// C.回写缓存：序列化后写入Redis， 设置5分钟过期时间
		jsonBytes, _ := json.Marshal(dbTask)
		RDB.Set(timeoutCtx, cacheKey, string(jsonBytes), 5*time.Second)

		c.JSON(http.StatusOK, gin.H{
			"status": "success",
			"source": "mysql_db",
			"data":   dbTask,
		})
	})

	// 启动web服务在一个单独的goroutine中，以便main函数可以监听信号进行优雅关闭
	srv := &http.Server{
		Addr:    ":8080",
		Handler: r,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			panic(fmt.Sprintf("Gin server failed to start: %v", err))
		}
	}()

	// 阻塞主goroutine，直到接收到中断信号
	// 这里可以添加信号监听逻辑，例如 os.Interrupt 或 syscall.SIGTERM
	// 为了演示优雅关闭，我们暂时直接调用 cancelGlobal 和 WG.Wait
	// 在实际生产环境中，这里应该是一个信号监听器
	cancelGlobal()
	WG.Wait()
	fmt.Println("所有后台任务已优雅关闭，程序退出。")
}

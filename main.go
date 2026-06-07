package main

import (
	"context" // 引入标准上下文，用来控制MySQL与Redis的请求生命周期
	"encoding/json"

	// 引入标准JSON序列化器，用来在Go结构体与Redis字符串之间做转换
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/locales/zh"
	ut "github.com/go-playground/universal-translator"
	"github.com/go-playground/validator/v10"
	zh_translations "github.com/go-playground/validator/v10/translations/zh"
	"github.com/redis/go-redis/v9" // 导入go-redis/v9驱动包
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// =====
// 1.全局双引擎：MySQL关系型数据库与Redis内存型缓存
// =====
var DB *gorm.DB         // 共享的MySQL连接池句柄
var RDB *redis.Client   // 共享的Redis连接池句柄
var trans ut.Translator // 翻译器

// =====
// 2.数据库实体定义(Entity)
// =====
type Task struct {
	gorm.Model
	TaskName  string  `gorm:"type:varchar(255);not null;index"`
	TaskType  string  `gorm:"type:varchar(255);not null"`
	Latitude  float64 `gorm:"not null"`
	Longitude float64 `gorm:"not null"`
	Status    bool    `gorm:"default:false"`
}

// =====
// 3.安全传输对象层(DTO)与GIS空间标签约束
// =====
type TaskCreateInput struct {
	TaskName  string  `json:"task_name" binding:"required,min=3"`
	TaskType  string  `json:"task_type" binding:"required,min=3"`
	Latitude  float64 `json:"latitude" binding:"required,chinalat"`
	Longitude float64 `json:"longitude" binding:"required,chinalng"`
}

// =====
// 4.自定义GIS空间校验器函数
// =====
func validateChinaLat(fl validator.FieldLevel) bool {
	lat := fl.Field().Float()
	return lat >= 3.86 && lat <= 53.55
}

func validateChinaLng(fl validator.FieldLevel) bool {
	lng := fl.Field().Float()
	return lng >= 73.66 && lng <= 135.05
}

// =====
// 5.核心初始化函数：打通所有网络与数据库链路
func initDatabases() {
	// A.建立MySQL连接
	dsn := "root:123456@tcp(127.0.0.1:3306)/todo_db?charset=utf8mb4&parseTime=True&loc=Local"
	var err error
	DB, err = gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		panic("CRITICAL:MySQL连接失败!" + err.Error())
	}
	DB.AutoMigrate(&Task{})
	fmt.Println("[OK]MySQL链路同步成功!")

	// B.建立Redis内存级连接
	RDB = redis.NewClient(&redis.Options{
		Addr: "127.0.0.1:6379", // 连向我们在WSL里跑的原生Linux Redis
	})

	// 强制发起Ping握手，确保Redis容器真实在线
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := RDB.Ping(ctx).Err(); err != nil {
		panic("CRITICAL:Redis连接失败!" + err.Error())
	}
	fmt.Println("[OK]Redis内存引擎链路成功!")
}

func initValidator() {
	if v, ok := binding.Validator.Engine().(*validator.Validate); ok {
		// 1.让报错的变量名自动转换为json tag的名字
		v.RegisterTagNameFunc(func(fld reflect.StructField) string {
			name := strings.SplitN(fld.Tag.Get("json"), ",", 2)[0]
			if name == "-" {
				return ""
			}
			return name
		})

		// 2.装载中文语料与翻译字典
		zhLocale := zh.New()
		uni := ut.New(zhLocale, zhLocale)
		trans, _ = uni.GetTranslator("zh")
		zh_translations.RegisterDefaultTranslations(v, trans)

		// 3.注册并绑定自定义空间Tag
		v.RegisterValidation("chinalat", validateChinaLat)
		v.RegisterTranslation("chinalat", trans, func(ut ut.Translator) error {
			return ut.Add("chinalat", "{0}超出中国有效纬度范围！", true)
		}, func(ut ut.Translator, fe validator.FieldError) string {
			t, _ := ut.T("chinalat", fe.Field())
			return t
		})

		v.RegisterValidation("chinalng", validateChinaLng)
		v.RegisterTranslation("chinalng", trans, func(ut ut.Translator) error {
			return ut.Add("chinalng", "{0}超出中国有效经度范围！", true)
		}, func(ut ut.Translator, fe validator.FieldError) string {
			t, _ := ut.T("chinalng", fe.Field())
			return t
		})
		fmt.Println("🚀 [OK] 自动翻译器与 GIS 自定义标签装载成功！")
	}

}

// =====
// 6.主程序与接口层
// =====

func main() {
	initDatabases()
	initValidator()

	r := gin.Default()

	// -----
	// [接口1]：创建任务（安全DTO过滤 + 参数自动翻译 + 缓存失效控制)
	// -----
	r.POST("/tasks", func(c *gin.Context) {
		// 建议将超时时间适当延长到 5 秒，防止 WSL 磁盘响应慢
		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel() // cancel是一个函数，调用它会立即停止计时器，并释放占用的资源
		c.Request = c.Request.WithContext(ctx)

		var input TaskCreateInput
		// A.校验
		if err := c.ShouldBindBodyWithJSON(&input); err != nil {
			errs, ok := err.(validator.ValidationErrors)
			if !ok {
				c.JSON(http.StatusBadRequest, gin.H{
					"status": "error",
					"error":  "验证器解析失败或JSON格式错误",
				})
				return
			} else {
				c.JSON(http.StatusBadRequest, gin.H{
					"status": "error",
					"error":  errs.Translate(trans),
				})
				return
			}
		}

		// B.安全过滤：将安全的DTO字段拷贝给实体， 防止批量赋值注入
		newTask := Task{
			TaskName:  input.TaskName,
			TaskType:  input.TaskType,
			Latitude:  input.Latitude,
			Longitude: input.Longitude,
			Status:    false,
		}

		// C.写入MySQL
		if err := DB.WithContext(ctx).Create(&newTask).Error; err != nil {
			c.JSON(500, gin.H{
				"status": "error",
				"error":  "MySQL写入失败:" + err.Error(),
			})
			return
		}

		// D.旁路缓存一致性：数据更新，直接“强制删除”可能存在的旧缓存，防止读到脏数据
		// 根据刚刚写入MySQL后自动生成的自增ID（newTask.ID),拼装出一个在Redis中唯一的标识符
		cacheKey := fmt.Sprintf(
			"tasks:detail:%d", newTask.ID,
		)

		// 金科玉律：只要发生了“写（增，删，改）”操作，都要对该数据的缓存执行“删除”动作
		RDB.Del(ctx, cacheKey) // 即使缓存不存在，Del操作也是安全的
		c.JSON(http.StatusCreated, gin.H{
			"status":  "success",
			"message": "成功创建任务",
			"data":    newTask,
		})

	})

	// ----
	// 核心接口 2：高并发冷热分离——单条任务详情查询（GET /tasks/:id)
	// ----
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

		// 声明高并发专用的超时Context
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()

		// 规划标准Redis键命名，格式是： "tasks:detail:{ID}"
		// 是一个唯一的字符串标识符，用于在 Redis 中定位和检索数据
		cacheKey := fmt.Sprintf(
			"tasks:detail:%d", id,
		)

		// ==== 步骤A : [热查询]去高速内存Redis里面找 ====
		cacheVal, err := RDB.Get(ctx, cacheKey).Result()

		// 状态1：缓存命中(Cache Hit)!
		if err == nil {
			fmt.Println("[Cache Hit]命中高速Redis内存!零磁盘I/O极速响应!")
			var cachedTask Task

			// 将Redis里的纯文本JSON字符串，反序列化还原为Go的结构体内存对象
			json.Unmarshal([]byte(cacheVal), &cachedTask)

			c.JSON(http.StatusOK, gin.H{
				"status": "success",
				"source": "redic_cache", // 标记数据来源于缓存
				"data":   cachedTask,
			})
			return
		}

		// 如果发生了除了"未找到(redis.Nil)”之外的其他网络报错，进行日志预警
		if !errors.Is(err, redis.Nil) {
			fmt.Printf("[Warning]Redis读取发生异常:%s\n", err.Error())
		}

		// ==== 步骤B:[冷查询]缓存未命中（Cache Miss),去硬盘MySQL里面爬====
		fmt.Println("[Cache Miss]缓存未命中，去MySQL硬盘里查询...")
		var dbTask Task
		dbErr := DB.WithContext(ctx).First(&dbTask, id).Error

		if dbErr != nil {
			if errors.Is(dbErr, gorm.ErrRecordNotFound) {
				c.JSON(http.StatusNotFound, gin.H{
					"status": "error",
					"error":  "该任务确实不存在",
				})
				return
			}
			c.JSON(500, gin.H{
				"status": "error",
				"error":  "MySQL数据库查询故障",
			})
		}

		// ==== 步骤C：[回写缓存]将最新数据回写到Redis，方便下一个访问的人
		// 将Go的结构体对象，序列化为JSON字符串，以方便写进Redis
		jsonBytes, _ := json.Marshal(dbTask)

		// 极其重要：必须设置生存过期时间（TTL)!这里设置为5分钟
		// 作用： 防止过期的冷垃圾数据长期霸占内存导致Redis发生内存溢出（OOM)崩溃！

		// 向Redis写入数据的核心命令 cacheKey是redis的键， string(json.Marshal(dbTask))是redis键值对的值
		RDB.Set(ctx, cacheKey, string(jsonBytes), 5*time.Minute)
		fmt.Println("💾 [Cache Rebuild] 已成功将最新数据回写至 Redis 内存，并注入 5分钟 倒计时过期。")

		c.JSON(http.StatusOK, gin.H{
			"status": "success",
			"source": "mysql_db", //标记数据来源于物理硬盘
			"data":   dbTask,
		})

	})

	// [接口3]：删除任务(先擦除硬盘，检查删除行数，最后再删缓存)
	r.DELETE("/tasks/:id/clean", func(c *gin.Context) {
		idStr := c.Param("id")
		id, err := strconv.Atoi(idStr)
		if err != nil || id <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{
				"status": "error",
				"error":  "ID Format Error",
			})
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		c.Request = c.Request.WithContext(ctx)

		// 先从硬盘里面物理擦除(以DB为主，确保成功后再清缓存)
		result := DB.Unscoped().Delete(&Task{}, id)
		if result.Error != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"status": "error",
				"error":  "物理清除失败:" + result.Error.Error(),
			})
			return
		}

		// RowAffected记录了这次删除SQL到底在硬盘里擦除了几行数据
		if result.RowsAffected == 0 {
			c.JSON(http.StatusNotFound, gin.H{
				"status": "error",
				"error":  "未找到对应记录",
			})
			return
		}

		// DB删除成功后再清理缓存
		cacheKey := fmt.Sprintf("tasks:detail:%d", id)
		RDB.Del(ctx, cacheKey)

		c.JSON(http.StatusOK, gin.H{
			"status":  "success",
			"message": "成功物理删除任务",
		})
	})

	// day8 今日挑战：获取分页任务列表
	r.GET("/tasks", func(c *gin.Context) {
		// 获取字符串类型的参数
		pageStr := c.DefaultQuery("page", "1")
		pageSizeStr := c.DefaultQuery("page_size", "10")
		taskType := c.Query("task_type")

		page, _ := strconv.Atoi(pageStr)
		pageSize, _ := strconv.Atoi(pageSizeStr)
		offset := (page - 1) * pageSize

		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		c.Request = c.Request.WithContext(ctx)

		// 分页任务下的redis命名
		cacheKey := fmt.Sprintf("tasks:list:type:%s:page:%d:size:%d", taskType, page, pageSize)

		// 热查询：去Redis查询
		cacheVal, err := RDB.Get(ctx, cacheKey).Result()

		//  状态1：缓存命中
		if err == nil {
			fmt.Println("[Cache Hit]命中高速Redis内存!")
			var cachedTask []Task // 必须使用切片[]Task,与写入端的类型保持严格对称！（会查到多条任务）

			// 反序列化 JSON->Task结构体
			json.Unmarshal([]byte(cacheVal), &cachedTask)
			c.JSON(http.StatusOK, gin.H{
				"status": "success",
				"source": "redis_cache",
				"data":   cachedTask,
			})
			return
		}

		// 如果发生了除了未找到以外的其他网络报错，日志预警
		if !errors.Is(err, redis.Nil) {
			fmt.Printf("[Warning]Redis读取发生异常:%s\n", err.Error())
		}

		// 状态2：缓存未命中
		// 冷查询：去MySQL物理硬盘里查询
		// 使用GORM动态查询链构建SQL,防止taskType为空时陷入“查询黑洞”
		fmt.Println("[Cache Miss]缓存未命中，去MySQL硬盘里查询...")

		query := DB.WithContext(ctx).Model(&Task{})
		if taskType != "" {
			query = query.Where("task_type = ?", taskType)
		}
		var dbTask []Task
		dbErr := query.Select("id", "task_name", "task_type", "status", "created_at").
			Order("created_at DESC").
			Limit(pageSize).
			Offset(offset).
			Find(&dbTask).
			Error

		if dbErr != nil {
			if errors.Is(dbErr, gorm.ErrRecordNotFound) {
				c.JSON(http.StatusNotFound, gin.H{
					"status": "ereror",
					"error":  "未找到对应记录",
				})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{
				"status": "error",
				"error":  "访问数据库失败",
			})
			return
		}

		// 回写缓存
		jsonBytes, _ := json.Marshal(dbTask)
		RDB.Set(ctx, cacheKey, string(jsonBytes), 2*time.Minute)
		fmt.Println("💾 [Cache Rebuild] 已成功将最新数据回写至 Redis 内存，并注入 2分钟 倒计时过期")

		c.JSON(http.StatusOK, gin.H{
			"status": "success",
			"source": "mysql_db",
			"data":   dbTask,
		})
	})

	// [接口4]：清空所有任务数据 (物理彻底擦除全表 + 同步清理 Redis 缓存， 主键ID变为1)
	r.DELETE("/tasks/all", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()

		// A. 使用 TRUNCATE 彻底清空表并重置自增 ID 计数器为 1
		// 注意：TRUNCATE 是 DDL 操作，会重置 AUTO_INCREMENT
		if err := DB.WithContext(ctx).Exec("TRUNCATE TABLE tasks").Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"status": "error",
				"error":  "清空数据库失败: " + err.Error(),
			})
			return
		}

		// B. 同步清理 Redis 缓存
		// 使用 Scan 模式查找所有以 tasks: 开头的键并删除，防止数据库空了但缓存还有旧数据
		iter := RDB.Scan(ctx, 0, "tasks:*", 0).Iterator()
		for iter.Next(ctx) {
			RDB.Del(ctx, iter.Val())
		}

		c.JSON(http.StatusOK, gin.H{
			"status":  "success",
			"message": "清理完成！数据表已重置（ID 将从 1 开始），并同步清空了相关 Redis 缓存",
		})
	})

	r.Run(":8080")
}

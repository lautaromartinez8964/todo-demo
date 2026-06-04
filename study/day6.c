package main

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/go-playground/validator/v10"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"context"
)

// =====
// 1.全局架构设计:数据库连接池指针
// =====
// DB是共享的指针，->堆内存中的*gorm.DB实例，其底层有标准库管理的连接池是协程安全的
var DB *gorm.DB

// ====
// 2.模型定义层
// ====

// Task是与MySQL中'tasks'表物理结构一一对应的实体模型
// 外部包（前端传来的)决不能直接往这个结构体里反序列化没过滤的JSON
type Task struct {
	// gorm.Model 注入了: ID(主键自增), CreateAt(创建时间)， UpdatedAt(更新时间)，  DeletedAt(软删除标记)
	gorm.Model
	TaskName  string  `gorm:"type:varchar(255);not null;index"` // 任务名字，长度 255，不能为空，创建普通索引
	TaskType  string  `gorm:"type:varchar(255);not null"`       // 算法类型，长度 255，不能为空
	Latitude  float64 `gorm:"not null"`                         // 纬度，不能为空（GORM 自动映射为 MySQL 的 double）
	Longitude float64 `gorm:"not null"`                         // 经度，不能为空
	Status    bool    `gorm:"default:false"`                    // 任务完成状态：true 代表已完成，false 代表未完成
}

// ====
// 3.数据传输对象层(DTO, Data Transfer Object)
// ====

// TaskCreateInput是前端专门用来提交“新建遥感任务”的DTO结构体
// 他只保留了前端被允许提交的四个核心字段，阻断了对ID,时间，状态的非法注入
type TaskCreateInput struct {
	TaskName  string  `json:"task_name" binding:"required,min=3"`
	TaskType  string  `json:"task_type" binding:"required,min=3"`
	Latitude  float64 `json:"latitude" binding:"required,gte=-90,lte=90"`
	Longitude float64 `json:"longitude" binding:"required,gte=-180,lte=180"`
}

// TaskPatchInput是前端专门用来提交“局部更新遥感任务”的DTO结构体
// 指针类型是为了防止go将未提交的，不用修改的字段赋为0值
// 如果前端没有传task_name,Go里的指针会解析为nil
// 如果前端传了task_nem = ""(需要修改为空)，Go里的指针会解析为指到空字符串的内存地址（不等于nil）
type TaskPatchInput struct {
	TaskName  *string  `json:"task_name" binding:"omitempty,min=3"`
	TaskType  *string  `json:"task_type" binding:"omitempty,min=3"`
	Latitude  *float64 `json:"latitude" binding:"omitempty,gte=-90,lte=90"`
	Longitude *float64 `json:"longitude" binding:"omitempty,gte=-180,lte=180"`
}

// ====
// 4.工业级初始化函数与Helper函数
// ====

// initDB在服务启动时运行，负责初始化唯一的数据库连接池并同步元数据表结构
func initDB() {
	dsn := "root:123456@tcp(127.0.0.1:3306)/todo_db?charset=utf8mb4&parseTime=True&loc=Local"
	var err error
	// 初始化GORM引擎， 并注入控制台SQL日志输出
	DB, err = gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		panic("CRITICAL ERROR:Database bottom link init failed!:" + err.Error())
	}

	// 自动表结构迁移，读取Task结构体的定义，在MySQL内部自动建立与更新tasks表
	DB.AutoMigrate(&Task{})
	fmt.Println("🚀 [OK] Global connection pool and table structure synchronization completed!")
}

// 工业级错误翻译器
// translateError 用作翻译器，将 validator 抛出的复杂英文对象，解构成对人类极度友好的中文报错信息
func translateError(err error) map[string]string {
	result := make(map[string]string)
	var errs validator.ValidationErrors
	if errors.As(err, &errs) {
		for _, f := range errs {
			switch f.Field() {
			case "TaskName":
				if f.Tag() == "required" {
					result["task_name"] = "任务名称不能为空"
				} else if f.Tag() == "min" {
					result["task_name"] = "任务名称长度不能小于3个字符"
				}
			case "TaskType":
				result["task_type"] = "算法任务类型为必填项"
			case "Latitude":
				result["latitude"] = "纬度超出物理有效范围[-90, 90]"
			case "Longitude":
				result["longitude"] = "经度超出物理有效范围[-180, 180]"
			}
		}
	} else {
		result["error"] = "非法的 JSON 格式数据请求"
	}
	return result
}

// ====
// 5.主程序或接口层
// ====

func main() {
	// 启动并加载数据库连接池
	initDB()

	r := gin.Default()

	// API1:创建遥感检测任务
	r.POST("/tasks", func(c *gin.Context) {
		var input TaskCreateInput
		// A.绑定前端jSON并执行struct校验
		if err := c.ShouldBindBodyWithJSON(&input); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"status": "error",
				"errors": translateError(err),
			})
			return
		}
		// B.DTO安全转换：经过严格过滤的DTO拷贝进实体，拒绝一切非授权字段注入
		newTask := Task{
			TaskName:  input.TaskName,
			TaskType:  input.TaskType,
			Latitude:  input.Latitude,
			Longitude: input.Longitude,
			Status:    false, // 业务逻辑：初始化的检测任务必须未完成
		}

		// C.调用共享DB指针执行插入(Finisher方法引发磁盘和网络I/O)
		if err := DB.Create(&newTask).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"status": "error",
				"errors": "写入数据初始化失败:" + err.Error(),
			})
			return
		}

		// D.successful response
		c.JSON(http.StatusOK, gin.H{
			"status": "success",
			"data":   newTask,
		})
	})

	// API2:获取分页过滤遥感任务列表(GET/tasks)
	r.GET("/tasks", func(c *gin.Context) {
		// A.获取查询和分页参数
		taskType := c.Query("task_type")
		pageStr := c.DefaultQuery("page", "1")
		pageSizeStr := c.DefaultQuery("page_size", "10")

		// B.分页安全验证与类型转换
		page, err1 := strconv.Atoi(pageStr)
		pageSize, err2 := strconv.Atoi(pageSizeStr)
		if err1 != nil || err2 != nil || page <= 0 || pageSize < 0 {
			c.JSON(http.StatusBadRequest, gin.H{
				"status": "error",
				"error":  "分页参数非法",
			})
			return
		}

		// 计算MySQL的偏移量LIMIT OFFSET公式
		// API接受的是page+pageSize（第几页，一页几条）
		// SQL/GORM需要的是offset(跳过前面多少条)+limit（取多少条）
		offset := (page - 1) * pageSize

		// C.动态SQL编译器链式组装(Method Chaining)
		query := DB.Model(&Task{})
		if taskType != "" {
			query = query.Where("task_type = ?", taskType)
		}

		// D.终结方法(Finisher Method),在这里向MySQL发送真正的网络请求
		var tasks []Task
		err := query.Select("id", "task_name", "task_type", "status", "created_at").
			Order("created_at DESC").
			Limit(pageSize).
			Offset(offset).
			Find(&tasks).Error
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"status": "error",
				"error":  "数据库查询失败:" + err.Error(),
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"status":    "success",
			"page":      page,
			"page_size": pageSize,
			"data":      tasks,
		})
	})

	// API3:更新任务状态为"已完成"
	r.PUT("/tasks/:id/complete", func(c *gin.Context) {
		idStr := c.Param("id")
		id, err := strconv.Atoi(idStr)
		if err != nil || id < 0 {
			c.JSON(http.StatusBadRequest, gin.H{
				"status": "error",
				"error":  "ID Format Error",
			})
			return
		}
		// A.验证该任务在数据库里是否存在
		var targetTask Task
		result := DB.First(&targetTask, id)
		if result.Error != nil {
			if errors.Is(result.Error, gorm.ErrRecordNotFound) {
				c.JSON(http.StatusNotFound, gin.H{
					"status": "error",
					"error":  "数据库里不存在该任务",
				})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{
					"status": "error",
					"error":  "和数据库连接错误",
				})
			}
			return
		}
		// B.安全更新：为防止零值更新被GORM结构体反射忽略，同时为了网络安全性
		// 强制指定更新字段并传入map写入
		// 用Select("status")起到白名单作用，明确告诉GORM只改status这一个字段
		// DB.Model告诉GORM指定操作的表明（根据结构体名推断）和锁定操作的主键（结构体实例里的id)
		result = DB.Model(&targetTask).Select("status").Updates(map[string]any{
			"status": true,
		})
		if result.Error != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"status": "error",
				"error":  "更新状态失败",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"status":  "success",
			"message": "该任务已被安全更新并标记为[已完成]",
		})

	})

	// ====
	// API4: 物理彻底从硬盘擦除一项任务(DELETE/tasks/:id/clean)
	// ====
	r.DELETE("/tasks/:id/clean", func(c *gin.Context) {
		idStr := c.Param("id")
		id, err := strconv.Atoi(idStr)
		if id < 0 || err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"status": "error",
				"error":  "ID Format Error",
			})
			return
		}

		// A.物理删除核心：必须使用Unscoped
		// Unscoped()告诉GORM编译出底层的物理删除SQL: DELETE FROM tasks WHERE id = ?
		result := DB.Unscoped().Delete(&Task{}, id)

		if result.Error != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"status": "error",
				"error":  "物理清除失败：" + result.Error.Error(),
			})
			return
		}

		// B.健壮性判断: RowsAffected记录了这次删除SQL到底在硬盘里擦除了几行数据
		if result.RowsAffected == 0 {
			c.JSON(http.StatusNotFound, gin.H{
				"status": "error",
				"error":  "未找到对应的历史记录，无需擦除",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"status":  "success",
			"message": "数据已经彻底从硬盘擦除，无法复原",
		})
	})

	// ====
	// 今日挑战:大厂级PATCH局部动态更新接口
	// ====
	// PUT接口通常是全量更新，或者指定写死字段更新
	// PATCH接口要求支持选择性更新（局部更新）
	//  。场景：前端如果发来：{"task_name":"新机场名字"}，我们只修改任务名字，不能动任务类型，更不能动经纬度
	//  。问题：如果用常规struct去接受,因为前端没有传递TaskType，go会反序列化出0值，直接更新就会把数据库里的task_type强行赋空
	r.PATCH("/tasks/:id/update", func(c *gin.Context) {
		idStr := c.Param("id")
		id, err := strconv.Atoi(idStr)
		if err != nil || id <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{
				"status": "error",
				"error":  "ID Format Error",
			})
			return
		}

		var updateInput TaskPatchInput
		if err := c.ShouldBindBodyWithJSON(&updateInput); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"status": "error",
				"errors": translateError(err),
			})
			return
		}

		// 使用ID查出对应的Task
		var targetTask Task
		result := DB.First(&targetTask, id)
		if result.Error != nil {
			if errors.Is(result.Error, gorm.ErrRecordNotFound) {
				c.JSON(http.StatusNotFound, gin.H{
					"status": "error",
					"error":  "数据库里不存在该任务",
				})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{
					"status": "error",
					"error":  "与数据库连接错误",
				})
			}
			return
		}

		// 构建更新字典，使用指针判断传入的是（更新为空 or 根本没变）
		// 如果用户传了，我们就将这个值赋给实体，并用map[string]any存起来
		updateMap := make(map[string]any)
		if updateInput.TaskName != nil {
			updateMap["task_name"] = updateInput.TaskName
		}
		if updateInput.TaskType != nil {
			updateMap["task_type"] = updateInput.TaskType
		}
		if updateInput.Latitude != nil {
			updateMap["latitude"] = updateInput.Latitude
		}
		if updateInput.Longitude != nil {
			updateMap["longitude"] = updateInput.Longitude
		}

		// 处理传入为空任务体时，应返回400
		if len(updateMap) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{
				"status": "error",
				"error":  "传入新任务为全空",
			})
			return
		}
		result = DB.Model(&targetTask).Updates(updateMap)

		if result.Error != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"status": "error",
				"error":  "更新任务失败",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"status":  "success",
			"message": "该任务已成功PATCH更新",
			"data":    targetTask,
		})
	})

	// 终极补充:SQL执行超时控制(Context Timeout)
	// 用GO原生Context穿透控制GORM
	// Go语言的Context(上下文)是用来控制生命周期和超时的，GORM支持直接传入带超时的Context
	// 场景：MySQL容器因为突然负载过高，或者发生了物理deadlock，导致某一条SQL查询卡死在数据库里
	// 如果没有设置超时，这个Find(&tasks)的网络I/O就会一直阻塞在那，导致当前的Goroutine永远无法结束
	r.GET("/tasks", func(c *gin.Context){
		// 1.创建一个2秒超时的上下文
		// c.Request.Context()代表继承当前HTTP请求的生命周期（如果用户中途关了浏览器，请求也会提前取消）
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel() // 确保函数结束时释放资源

		var tasks []Task

		// 2.将超时context注入到当前的GORM查询中
		// 如果MySQL在2秒内没有返回数据， GORM会立刻掐断连接， 返回context.DeadlineExceeded错误
		err := DB.WithContext(ctx).Where("latitude > ?", 30.0).Or("task_name LIKE ?", "%机场%").Find(&tasks).Error

		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				c.JSON(http.StatusGatewayTimeout, gin.H{"error": "数据库查询超时"})
				return
			}else{
				c.JSON(http.StatusInternalServerError, gin.H{"error": "数据库查询失败"})
				return
			}
		}
	})

	r.Run(":8080")
}

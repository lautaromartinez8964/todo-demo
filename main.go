package main

import (
	"errors" //引入标准库errors, 用于类型转换判断
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10" // 引入Gin底层的验证包
)

// ---1.结构体定义---
// 地理坐标子结构体

type Coordinate struct {
	// gratitude must in -90 - 90； logtitude must in -180 ~ 180
	// 反射机制的运用：用反引号包裹的字段标签，能让Go自动匹配JSON key,告诉Gin的解析器：
	// 如果前端传来的JSON有一个字段标签名字是task_name,就把他塞进Go语言里的TaskName字段里
	// 声明式校验：通过标签直接定义约束，无需在业务代码中手写 if latitude < -90 || latitude > 90 之类的判断
	// Gin的ShouldBindJSON会自动完成校验并返回友好错误
	// 结构体字段首字母必须大写
	Latitude  float64 `json:"latitude" binding:"required,gte=-90,lte=90"`
	Longitude float64 `json:"longitude" binding:"required,gte=-180,lte=180"`
}

// 遥感检测任务主结构体
type TaskInput struct {
	TaskName   string     `json:"task_name" binding:"required,min=3"`
	TaskType   string     `json:"task_type" binding:"required"`
	Coordinate Coordinate `json:"coordinate" binding:"required"` // 嵌套地理坐标
}

// 用户注册输入结构体
type RegisterInput struct {
	Username string `json:"username" binding:"required,min=5"`
	Email    string `json:"email" binding:"required,email"`    // 自动校验必须是合法邮箱
	Password string `json:"password" binding:"required,min=6"` // 今日挑战： 增加限制条件并进行翻译
}

// --- 2.工业级错误翻译器(Helper)---

// 将验证器抛出的英文乱码翻译成优雅的中文键值对
func translateError(err error) map[string]string {
	result := make(map[string]string)

	//使用errors.As判断err是否是validator.ValidationErrors类型（是否是验证其抛出的结构化错误）
	var errs validator.ValidationErrors
	if errors.As(err, &errs) {
		for _, f := range errs {
			// f.Field()拿到发生校验错误的字段名
			// f.Tag()拿到未通过的校验标签名（如required, email, gte等)
			switch f.Field() {
			case "TaskName":
				if f.Tag() == "required" {
					result["task_name"] = "任务名称不能为空"
				} else if f.Tag() == "min" {
					result["task_name"] = "任务长度不能小于三个字符"
				}
			case "TaskType":
				result["task_type"] = "任务类型不能为空"
			case "Latitude":
				if f.Tag() == "required" {
					result["latitude"] = "纬度不能为空"
				} else {
					result["latitude"] = "纬度必须在-90到90之间"
				}
			case "Longitude":
				if f.Tag() == "required" {
					result["longitude"] = "经度不能为空"
				} else {
					result["longitude"] = "经度必须在-180到180之间"
				}
			case "Username":
				if f.Tag() == "required" {
					result["username"] = "用户名不能为空"
				} else if f.Tag() == "min" {
					result["username"] = "用户名长度不能小于5个字符"
				}
			case "Email":
				if f.Tag() == "required" {
					result["email"] = "邮箱不能为空"
				} else if f.Tag() == "email" {
					result["email"] = "邮箱格式不正确"
				}
			case "Password":
				if f.Tag() == "required" {
					result["password"] = "密码不能为空"
				} else if f.Tag() == "min" {
					result["password"] = "密码长度不能小于6个字符"
				}
			default:
				result[f.Field()] = "字段校验未通过:" + f.Tag()

			}
		}
	} else {
		//如果不是验证器错误，说明前端发送了破损的JSON纯文本导致解析彻底失败
		result["error"] = "解析失败,请发送合法的JSON数据"
	}
	return result
}

// ---3.主函数路由绑定---
func main() {
	r := gin.Default()
	// 接口1：遥感监测任务提交
	r.POST("/tasks", func(c *gin.Context) {
		var input TaskInput

		// 传入指针&input， 让Gin的Context能够直接往内存里填入解析后的数据
		// 千万要传指针，否则如果是值拷贝， Gin解析的数据写进临时副本，函数结束拷贝就被销毁
		if err := c.ShouldBindJSON(&input); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"status": "error",
				"errors": translateError(err),
			})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"status":        "success",
			"message":       "rs task submit successfully, entering dissolving queue",
			"received_data": input,
		})
	})

	// 接口2：用户注册
	r.POST("/register", func(c *gin.Context) {
		var input RegisterInput
		if err := c.ShouldBindJSON(&input); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"status": "error",
				"errors": translateError(err),
			})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"status":        "success",
			"message":       "user successfully register!",
			"received_data": input,
		})
	})

	r.Run(":8080")
}

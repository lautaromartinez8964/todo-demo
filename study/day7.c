package main

import (
	// 引入标准上下文，用于后续的超时控制
	// 引入标准错误包，进行类型断言 [1]
	"fmt"

	"context" // 引入标准上下文，用于后续的超时控制
	"reflect" // 引入反射包，用于获取结构体字段的tag
	"strings" // 引入字符串处理包，用于处理tag

	// 引入标准错误包，进行类型断言 [1]
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/locales/zh"                                    // 1. 导入中文本地化 CLDR 语料库
	ut "github.com/go-playground/universal-translator"                       // 2. 导入通用翻译管理器
	"github.com/go-playground/validator/v10"                                 // 3. 导入核心验证器引擎
	zh_translations "github.com/go-playground/validator/v10/translations/zh" // 4. 导入官方英文->中文翻译字典
)

// =====
// 1.全局变量设计（全局翻译器）
// =====
// trans承载着加载好的中文翻译字典，由于其底层设计，它是goroutine安全的，全局所有API共享
var trans ut.Translator

// =====
// 2.遥感数据传输模型(DTO)与GIS空间约束Tag设计
// =====
type TaskCreateInput struct {
	TaskName string `json:"task_name" binding:"required,min=3"`
	TaskType string `json:"task_type" binding:"required"`

	// chinaLat:我们自定义注册的"中国有效纬度限制器"
	Latitude float64 `json:"latitude" binding:"required,chinalat"`
	// chinaLat: 我们自定义注册的“中国有效经度限制器"
	Longitude float64 `json:"longitude" binding:"required,chinalong"`
}

// 今日挑战：Bounding Box范围合理性交叉校验器
// 满足两个绝对不能颠倒的几何关系：MinLat < MaxLat, MinLong < MaxLong
type ROIInput struct {
	ROIName string  `json:"roi_name" binding:"required,min=3"`
	MinLat  float64 `json:"min_lat" binding:"required,chinalat"`
	MaxLat  float64 `json:"max_lat" binding:"required,chinalat"`
	MinLong float64 `json:"min_long" binding:"required,chinalong"`
	MaxLong float64 `json:"max_long" binding:"required,chinalong"`
}

// =====
// 3.自定义字段级校验器函数
// =====

// validateChinaLat 校验传入的纬度是否落在我国物理领土范围内
func validateChinaLat(fl validator.FieldLevel) bool {
	lat := fl.Field().Float()
	return lat >= 3.86 && lat <= 53.55 // 返回true代表校验通过
}

// validateChinaLng 校验传入的经度是否...
func validateChinaLong(fl validator.FieldLevel) bool {
	lng := fl.Field().Float()
	return lng >= 73.66 && lng <= 135.05
}

// =====
// 4.自定义结构体级校验器函数(Struct-level validator)
// =====

// validateNoFly 负责跨字段校验：如果经纬度落入我国敏感的禁飞区范围，直接熔断报错
func validateNoFly(sl validator.StructLevel) {
	// 1.通过sl.Current()拿到当前正在被校验的结构体反射值，并通过类型断言强行转换为TaskCreateInput
	input := sl.Current().Interface().(TaskCreateInput)

	// 2.业务规则定义：假设[39.00-41.00, 115.00-117.00]北京周边区域为禁飞区
	if input.Latitude >= 39.00 && input.Latitude <= 41.00 && input.Longitude >= 115.00 && input.Longitude <= 117.00 {
		// 跨字段人为报错：如果禁飞区，把错误人为挂载在”TaskName"这个字段上抛出
		// 参数说明:错误实际值， 字段JSON名，字段结构体名， 自定义Tag名字，附加参数
		sl.ReportError(input.TaskName, "task_name", "TaskName", "nofly", "")
	}
}

// day7测试
func validateROI(sl validator.StructLevel) {
	input := sl.Current().Interface().(ROIInput)
	if input.MinLat >= input.MaxLat {
		sl.ReportError(input.MinLat, "min_lat", "MinLat", "latrange", "")
	}
	if input.MinLong >= input.MaxLong {
		sl.ReportError(input.MinLong, "min_long", "MinLong", "lngrange", "")
	}
}

// =====
// 5.核心初始化函数:注册翻译器与自定义多层校验器
// =====

func initValidator() {
	// A.拿到底层Gin框架启动时创建的原生validator.
	if v, ok := binding.Validator.Engine().(*validator.Validate); ok {

		// 0. 注册 TagNameFunc：让验证器能够识别 json 标签作为字段名
		v.RegisterTagNameFunc(func(fld reflect.StructField) string {
			name := strings.SplitN(fld.Tag.Get("json"), ",", 2)[0]
			if name == "-" {
				return ""
			}
			return name
		})

		// B.加载中文CLDR语料， 并实例化通用翻译管理器
		zhLocale := zh.New()              // 实例化中文语料包
		uni := ut.New(zhLocale, zhLocale) // 将其作为缺省和备用语言注册进 UT 翻译管理器

		var found bool
		trans, found = uni.GetTranslator("zh") // 提取出我们的中文翻译
		if !found {
			panic("CRITICAL: 初始化中文翻译器失败！")
		}

		// C.一键将官方提供的"英文->中文"翻译字典全部写进我们的翻译器中
		if err := zh_translations.RegisterDefaultTranslations(v, trans); err != nil {
			panic("CRITICAL: 初始化中文翻译器失败！")
		}

		// C1. 手动注册字段名的中文翻译（用于自定义校验报错中的 {0} 占位符）
		_ = trans.Add("latitude", "纬度", true)
		_ = trans.Add("longitude", "经度", true)

		// === 1.注册自定义字段级校验与翻译 ===
		// D.注册chinalat校验器
		v.RegisterValidation("chinalat", validateChinaLat)
		// E.为chinalat注册翻译模板，如果错误，自动翻译
		v.RegisterTranslation("chinalat", trans, func(ut ut.Translator) error {
			return ut.Add("chinalat", "{0}超出中国有效纬度物理范围！", true) // {0} 会被替换为翻译后的字段名
		}, func(ut ut.Translator, fe validator.FieldError) string {
			// fe.Field() 现在返回的是 "latitude"
			fName, _ := ut.T(fe.Field()) // 将 "latitude" 翻译为 "纬度"
			t, _ := ut.T("chinalat", fName)
			return t
		})

		// F.注册chinalng校验器
		v.RegisterValidation("chinalong", validateChinaLong)
		// G.为chinalong注册翻译模板
		v.RegisterTranslation("chinalong", trans, func(ut ut.Translator) error {
			return ut.Add("chinalong", "{0}超出中国有效经度物理范围！", true) // 占位符在运行时会被自动替换成经过翻译的字段名称(如经度)
		}, func(ut ut.Translator, fe validator.FieldError) string {
			fName, _ := ut.T(fe.Field()) // 将 "longitude" 翻译为 "经度"
			t, _ := ut.T("chinalong", fName)
			return t
		})

		// === 2.注册自定义结构体级校验器 ===
		// G.注册结构体级别关联校验：告诉验证器，凡是校验TaskCreateInput结构体的，必须额外跑一次我们写的validateNoFly跨字段函数
		v.RegisterStructValidation(validateNoFly, TaskCreateInput{})

		// H.为结构体级校验抛出的"nofly"Tag注册专属中文翻译
		v.RegisterTranslation("nofly", trans, func(ut ut.Translator) error {
			return ut.Add("nofly", "该任务的设定坐标落入了禁飞区，拒绝创建任务！", true)
		}, func(ut ut.Translator, fe validator.FieldError) string {
			t, _ := ut.T("nofly") // 模板里没占位符，就不要传参数
			return t
		})

		// day7挑战：注册roi经纬度错误结构体级校验器
		v.RegisterStructValidation(validateROI, ROIInput{})
		v.RegisterTranslation("latrange", trans, func(ut ut.Translator) error {
			return ut.Add("latrange", "ROI 区域最小纬度不能大于最大纬度，请检查经纬度区间输入！", true)
		}, func(ut ut.Translator, fe validator.FieldError) string {
			t, _ := ut.T("latrange")
			return t
		})
		v.RegisterTranslation("lngrange", trans, func(ut ut.Translator) error {
			return ut.Add("lngrange", "ROI 区域最小经度不能大于最大经度，请检查经纬度区间输入！", true)
		}, func(ut ut.Translator, fe validator.FieldError) string {
			t, _ := ut.T("lngrange")
			return t
		})

		fmt.Println("[OK]自动翻译引擎&自定义多级GIS空间约束器初始化成功！")
	}
}

// =====
// 6.主控制与路由层(Controllers & Router)
// =====

func main() {
	// 加载验证器
	initValidator()

	r := gin.Default()

	r.POST("/tasks", func(c *gin.Context) {
		// 注入Go并发控制的Context Timeout: 任何API与校验，最大出力时间不超过2s，超时自动熔断
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()

		var input TaskCreateInput

		// 将带超时的上下文绑定进当前请求逻辑中
		c.Request = c.Request.WithContext(ctx)

		// 执行JSON绑定与反射参数验证
		if err := c.ShouldBindJSON(&input); err != nil {
			// 将通用err断言为validator专属的ValidationErrors错误切片
			errs, ok := err.(validator.ValidationErrors)
			if !ok { //如果是请求体格式问题，不是传入的数据不符合人为设定：
				c.JSON(http.StatusBadRequest, gin.H{
					"status": "error",
					"error":  "非法的JSON数据请求体格式",
				})
				return
			}
			// 一行代码，全自动翻译！不再需要switch case
			translateErrors := errs.Translate(trans)

			c.JSON(http.StatusBadRequest, gin.H{
				"status": "error",
				"errors": translateErrors,
			})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"status":  "success",
			"message": "恭喜！该遥感图像任务已经顺利通过国家边界范围校验与禁飞区阻断校验！",
			"data":    input,
		})
	})

	//day7 挑战：提交roi接受ROIInput的接口
	r.POST("/rois", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()

		var roiInput ROIInput
		c.Request = c.Request.WithContext(ctx)

		if err := c.ShouldBindJSON(&roiInput); err != nil {
			errs, ok := err.(validator.ValidationErrors)
			if !ok {
				c.JSON(http.StatusBadRequest, gin.H{
					"status": "error",
					"error":  "非法的JSON数据请求体格式",
				})
				return
			}
			translateErrors := errs.Translate(trans)

			c.JSON(http.StatusBadRequest, gin.H{
				"status": "error",
				"errors": translateErrors,
			})
			return
		} else {
			c.JSON(http.StatusOK, gin.H{
				"status":  "success",
				"message": "ROI已经成功输入",
				"data":    roiInput,
			})
		}

	})
	r.Run(":8080")
}

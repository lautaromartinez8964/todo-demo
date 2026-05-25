package main

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

func main() {
	r := gin.Default()

	// --- 接口1：演示Query参数与类型转换 ---
	// 需求：用户传入name和age，我们要判断该用户是否成年(age >= 18)
	// 访问路径示例： /user?name=Jack&age=20
	r.GET("/user", func(c *gin.Context) {
		// 1.获取字符串类型的参数
		name := c.DefaultQuery("name", "Guest") // 获取name字符串，当请求中未提供name参数时，默认返回Guest
		ageStr := c.Query("age")                // 获取age字符串

		// 2.核心痛点: ageStr是string类型（比如"20"), 我们无法直接与18进行比较
		// 必须使用strconv.Atoi将其转为int
		age, err := strconv.Atoi(ageStr)

		// 3.健壮性处理：如果用户没传age， 或者传了非数字(如age=abc)， 转换就会报错
		if err != nil {
			// 如果报错，必须立即拦截，返回400（Bad Request）告诉前端参数错了
			c.JSON(http.StatusBadRequest, gin.H{
				"error":   "age must be effective int",
				"details": err.Error(),
			})
			return // 非常重要，必须return，否则代码会向下执行,既给浏览器返回 400 Error， 又返回200 Success
		}

		// 4.业务逻辑处理: 此时age已经是int类型了
		isAdult := false
		if age >= 18 {
			isAdult = true
		}

		// 5.return results
		c.JSON(
			http.StatusOK,
			gin.H{
				"username": name,
				"age":      age,
				"is_adult": isAdult,
			})

	})

	//---接口2:演示Path参数---
	// 需求: 根据图书ID获取图书详情，ID嵌入在路径中
	// 访问路径示例： /book/1001
	r.GET("/book/:id", func(c *gin.Context) {
		bookIDStr := c.Param("id")
		bookID, err := strconv.Atoi(bookIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":   "book id must be effective int",
				"details": err.Error(),
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"book_id": bookID,
			"name":    "Three Kingdoms",
			"author":  "Sun Tzu",
		})
	})

	// 练习1：带数字转换的搜索接口
	// 新增一个/search路由，支持获取Query参数q和必须是int类型的页码page
	r.GET("/search", func(c *gin.Context) {
		q := c.DefaultQuery("q", "default")
		pageStr := c.DefaultQuery("page", "1")
		page, err := strconv.Atoi(pageStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":  "page must be effective int",
				"detail": err.Error(),
			})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"query": q,
			"page":  page,
		})
	})

	// 练习2：带范围限制的计算接口
	// 需求：新增一个路径参数接口 /squre/:num
	// 规则：num是数字，限制在100以内
	r.GET("/square/:num", func(c *gin.Context) {
		numStr := c.Param("num")
		num, err := strconv.Atoi(numStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":  "num must be effective int",
				"detail": err.Error(),
			})
			return
		}
		if num > 100 {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "num too large, must limit to 100",
			})
			return
		}
		result := num * num
		c.JSON(http.StatusOK, gin.H{
			"num":    num,
			"result": result,
		})

	})

	r.Run(":8080")

}

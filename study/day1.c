
package main

import (
	"net/http"
	"strconv"
	"github.com/gin-gonic/gin"
)

func main() {
	// 1.初始化引擎
	// 创建一个默认的Gin引擎，赋值给变量r（Router， 路由器的缩写）
	// 默认帮我们雇佣了两个隐形员工（中间件Middleware)
	//  .Logger(日志员)：每次有人访问，都会在控制台打印一行日志，告诉你谁访问了，用了多长时间（对调试很有用）
	//  .Recovery(急救员)：如果业务代码不小心写崩了，会把错误拦截住让整个Web服务继续运行，而不是让整个程序直接闪退
	r := gin.Default()

	// 2.绑定路由与业务逻辑
	// 字面意思：告诉前台经理r, 如果有人用GET方法敲/hello这个门，就去执行后面大括号里的函数
	// 深入理解:
	//  . GET: HTTP动作
	//  . "/hello" : URL路由
	//  . func(): 匿名函数，具体的业务逻辑
	//  。 c *gin.Context:
	//     上下文(Context)是Gin最核心的数据结构，没有之一
	//     可以把他理解为顾客的点单盘
	//     既装着顾客送来的东西（请求的参数，Header， Body， 浏览器的IP等)
	//     也装着我们要送回给顾客的东西（我们要返回给他的网页，图片或者JSON)
	//     在处理请求时，所有的操作都在这个c上面

	//c.JSON()..
	// 字面意思：向账单c写入响应， 返回状态码200， 并吐出一段JSON数据
	// 深入理解:
	//  . c.JSON: 我们调用Context对象的JSON方法，告诉他，把我们要返回的内容，用JSON格式打包发给浏览器
	//  . 200: 这是HTTP状态码，在HTTP规则中，200代表OK, 代表服务器成功处理了请求
	//  . gin.H: 是gin提供的一个代替键值对 map[string]interface{}的方法，里面的内容会自动转为标准JSON文本
	r.GET("/hello", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"message": "Hello Go Web!",
		})
	})

	r.GET("/ping", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"status": "alive",
		})
	})
	
	// 3.启动服务
	// 字面:让前台经理在大门口营业，监听8080端口
	// 深入理解：
	//   .如果不写参数，r.Run(), Gin默认也会占用8080
	//   .这行代码一旦运行，程序就会阻塞（停在这里），开始死循环监听网络，一直到手动按ctrl+c才停止
	r.Run(":8080")

}

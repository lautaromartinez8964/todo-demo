package main

import (
	"fmt"
)

// 实验1：有名返回值的修改
func deferTiming() (result int) {
	defer func() {
		result++ // 第二步：在defer里修改了名字为result的返回值变量
	}()
	return 1 // 第一步：result = 1； 第二步：执行defer(result变为2)； 第三步：返回result
}

// 实验 2：无名返回值的修改（其实底层有一个隐藏的无名变量）
func deferNoName() int {
	result := 1
	defer func() {
		result++ // 第二步：修改了局部变量 result，但由于返回值变量是无名的，修改不影响已经赋好值的返回值！
	}()
	return result // 第一步：隐式返回值变量 = result (即 1); 第二步：执行 defer (局部变量 result 变为 2); 第三步：返回隐式返回值变量 (仍为 1)
}

func main() {
	fmt.Println("有名返回值执行结果:", deferTiming()) // 应该是多少？
	fmt.Println("无名返回值执行结果:", deferNoName()) // 应该是多少？
}

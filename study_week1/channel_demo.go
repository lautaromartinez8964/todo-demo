package main

import (
	"fmt"
	"time"
)

// 实验1:向一个已经关闭的channel写入数据
func triggerClosePanic() {
	ch := make(chan string, 2)
	ch <- "Image A"
	close(ch) // 关闭传送带

	fmt.Println("成功关闭通道，现在尝试往里面写数据...")
	ch <- "Image B" // 触发Panic!
}

// 实验2： 操作一个没有初始化的nil channel
func triggerNilBlock() {
	var ch chan string // 声明了，但没有make，是nil channel

	go func() {
		fmt.Println("goroutine启动:尝试从nil channel读数据...")
		<-ch // 永远阻塞在这里
		fmt.Println("这行不会执行")
	}()

	time.Sleep(100 * time.Millisecond)
	fmt.Println("主协程结束，nil channel 的接收端被卡死了。")
}

func man() {
	triggerClosePanic()
	triggerNilBlock()
}

package main

import (
	"context"
	"fmt"     // 导入标准库同步包，Mutex活在这里
	"runtime" // 导入运行时包，我们可以用它来查当前内存里有多少个活着的协程
	"time"
)

// 任务结构体
type RSTask struct {
	ID   int
	Name string
}

// 泄露的子goroutine：接受管道
func leakWorker(ch chan int) {
	fmt.Println("[Leakly Worker]协程已启动， 准备从管道里拿数据")

	// 致命漏洞：尝试从管道里读取数据
	// 但如果外面没有任何人往这个管道里写数据，这个协程就会永远阻塞在这里，无法退出，造成内存泄漏
	val := <-ch
	fmt.Println("👶 [Leaky Worker] 拿到了数据并退出:", val)
}

// 安全的子goroutine：接受一个context实例
func safeWorker(ctx context.Context, id int) {
	fmt.Printf("👶 [Safe Worker-%d] 协程启动成功，在后台执行遥感图分析...\n", id)
	for {
		select {
		// 核心：随时盯着父级发送的取消命令
		// 当父级调用了cancel()时，ctx.Done()通道会被瞬间关闭，trigger这个case！
		case <-ctx.Done():
			fmt.Printf("👶 [Safe Worker-%d] 收到父级销毁指令: %s，正在自我清理资源并优雅退出！\n", id, ctx.Err())
			return // 必须安全return，goroutine在内存里才会被彻底回收销毁
		default:
			// 模拟正常的后台工作
			time.Sleep(200 * time.Millisecond)
		}
	}
}
func main() {
	// 一.整场goroutine生产者-消费者任务
	// 创建一个带缓存的通道，最大容量为3
	// 相当于传送带上最多能同时放3个包裹，满了之后生产者会阻塞等待
	taskChan := make(chan RSTask, 3)

	// 2.启动生产者协程： 模拟卫星源源不断解析并发送任务
	go func() {
		for i := 1; i < 5; i++ {
			task := RSTask{
				ID:   i,
				Name: fmt.Sprintf("遥感图像-%d号", i),
			}
			fmt.Printf("🛰️  [生产者] 解析出了新图像，准备送上流水线: %s\n", task.Name)

			// 写入管道： 使用 <- 符号，将task送入管道
			taskChan <- task
			time.Sleep(100 * time.Millisecond) // 模拟解析耗时
		}

		// 核心工程规范：生产者生产完毕后，必须主动关闭管道！
		// 告知消费者：货物送完了，后面没有了，你处理完剩下的就行
		close(taskChan)
		fmt.Println("🛰️  [生产者] 今天的卫星图全部解析完毕，关闭流水线！")
	}()

	// 3.启动[消费者协程]:在主线程里，盯着管道处理任务
	fmt.Println("[消费者]YOLO算法专家已就位，开始观察流水线")

	// 使用for range循环，可以自动，安全地从管道里不断掏出数据
	// 当管道为空时，消费者会阻塞等待，当管道被生产者close且数据被掏空后，循环会自动安全终止！
	for task := range taskChan {
		fmt.Printf("[消费者]抢到了货:%s, 开始执行YOLO推理检测...\n", task.Name)
		time.Sleep(300 * time.Millisecond) // 模拟复杂的算法计算耗时
		fmt.Printf("[消费者]处理完毕：%s\n", task.Name)
	}

	fmt.Println("🎉 全局流水线安全、顺畅运转完毕！没有写任何一行锁，天然安全！")

	// 二.内存杀手:Goroutine内存泄露
	// 1.打印当前系统里的goroutine数量
	fmt.Println("初始时刻,存活的Goroutine数量为:", runtime.NumGoroutine())

	// 2.制造泄露：循环5次，每次启动一个会永久卡死的goroutine
	for range 5 {
		// 创建一个没有缓冲区的管道，且没有人往里面写数据
		brokenChan := make(chan int)

		// 启动goroutine
		go leakWorker(brokenChan)
	}

	// 睡眠1s，让子goroutine全部跑起来卡斯状态
	time.Sleep(1 * time.Second)

	// 3. 再次观察协程数量！
	fmt.Println("🚨 [事故现场] 当前存活的 Goroutine 数量为:", runtime.NumGoroutine())
	fmt.Println("💀 警报！多出的 5 个协程已经永久在后台卡死，内存已发生无声泄露！")

	// 安全的解决方案：使用Context优雅控制子goroutine生命周期
	// 一律使用context.Context, 在父协程（主线程）中对子协程发送“销毁指令”
	// 1.创建一个带“取消功能”的Context上下文
	// ctx是令牌，cancel 是能trigger取消命令的函数指针
	ctx, cancel := context.WithCancel(context.Background())

	// 2.安全启动5个子goroutine，将ctx令牌传给他们
	for i := 1; i <= 5; i++ {
		go safeWorker(ctx, i)
	}

	time.Sleep(1 * time.Second)
	fmt.Println("📊 [运行中] 当前存活的 Goroutine 数量为:", runtime.NumGoroutine())

	// 3.核心：主协程(父级)发出广播销毁指令
	fmt.Println("\n📢 [Master] 发出全局广播：任务结束，通知后台所有协程安全自毁！")
	cancel() // 调用cancel函数，所有子协程的ctx.Done() 会同时接受信号

	// 睡眠1s，给子goroutine一些时间，让他们跑完return
	time.Sleep(1 * time.Second)

	// 4. 再次确认内存，看看有没有残留！
	fmt.Println("\n📊 全局自毁结束，当前存活的 Goroutine 数量为:", runtime.NumGoroutine())
	fmt.Println("🎉 [完美自救] 所有的僵尸协程已经全部在内存中安全退场、彻底销毁！")
}

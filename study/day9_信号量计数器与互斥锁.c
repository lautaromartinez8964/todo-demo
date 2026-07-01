package main

import (
	"fmt"
	"sync"
)

type SafeSliceCounter struct {
	mu        sync.Mutex
	dataSlice []int
}

func (s *SafeSliceCounter) Write(val int) {
	// 加锁
	s.mu.Lock()

	// 解锁
	defer s.mu.Unlock()

	s.dataSlice = append(s.dataSlice, val)
}
func main() {
	sliceCounter := SafeSliceCounter{
		dataSlice: make([]int, 0),
	}

	// 1.声明一个等待组(sync.WaitGroup, 信号量计数器)
	// 它就像一个“点名册”。启动一个协程，计数器加 1；协程运行结束，计数器减 1。
	// 主协程（main）一直阻塞等待，直到计数器归零，立刻秒级复苏并往下执行，不多睡一毫秒，也绝不提早退出
	// 这样就不用Time.Sleep(1*time.Second)等待子goroutine运行结束了
	var wg sync.WaitGroup

	// 2.告诉计数器:我们将启动2个子goroutine
	wg.Add(2)

	// 协程1
	go func() {
		// 核心规范：使用defer, 确保goroutine无论发生什么，结束时都必须调用Done()让计数器-1
		defer wg.Done()
		for i := 0; i < 10000; i++ {
			sliceCounter.Write(i)
		}
	}()

	// 协程2
	go func() {
		defer wg.Done()
		for i := 0; i < 10000; i++ {
			sliceCounter.Write(i)
		}
	}()

	// 3.阻塞等待:代替time.Sleep! 主goroutine会卡在这一行，到两个子goroutine全部运行完Done()，瞬间唤醒
	wg.Wait()

	//读取时也需要枷锁保护
	sliceCounter.mu.Lock()
	defer sliceCounter.mu.Unlock()
	fmt.Printf("运行结束,预期长度:20000,实际长度:%d\n", len(sliceCounter.dataSlice))
}

package main

import (
	"sync"
	"time"
)

func main2() {
	m := make(map[string]int)
	var lock sync.Mutex

	// 10个协程并发写入同一个map
	for i := 0; i < 10; i++ {
		go func() {
			lock.Lock()
			m["key"] = 1
			lock.Unlock()
		}()
	}

	// 故意不加锁去读这个map， 制造“读写冲突”
	go func() {
		_ = m["key"]
	}()

	time.Sleep(50 * time.Millisecond)
}

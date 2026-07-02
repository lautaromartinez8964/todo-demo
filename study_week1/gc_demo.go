package main

import (
	"runtime"
	"time"
)

// 模拟不断生成大量临时测绘瓦片数据
func allocateTiles() {
	_ = make([]byte, 10*1024*1024) // 每次生成10mb的临时内存
}

func main() {
	for i := 0; i < 5; i++ {
		allocateTiles()
		runtime.GC() // 手动强制触发一次大扫除
		time.Sleep(100 * time.Millisecond)
	}
}

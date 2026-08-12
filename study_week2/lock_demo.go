package main

import (
	"fmt"

	"sync"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Task 遥感任务表模型

type Task struct {
	ID      uint `gorm:"primaryKey"`
	Title   string
	Likes   int
	Version int
}

func main() {
	//连接Docker中的MySQL
	dsn := "root:geo_secret_123@tcp(127.0.0.1:3306)/geo_platform?charset=utf8mb4&parseTime=True&loc=Local"
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		panic("数据库连接失败:" + err.Error())
	}

	// 自动迁移建表并初始化数据
	db.AutoMigrate(&Task{})
	db.Where("id = ?", 1).Assign(Task{Title: "Landsat8 土地覆盖", Likes: 0}).FirstOrCreate(&Task{ID: 1})

	fmt.Println("===实验1:无并发控制(验证写覆盖Bug)====")
	// reset点赞数为0
	db.Model(&Task{}).Where("id = ?", 1).Update("likes", 0)

	var wg sync.WaitGroup
	concurrentCount := 50 // 50个并发协程

	for i := 0; i < concurrentCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// ❌错误做法，没加锁读写
			var t Task
			db.Where("id = ?", 1).First(&t)
			t.Likes++
			db.Save(&t)
		}()
	}
	wg.Wait()

	var unpessimisticTask Task
	db.Where(" id = ?", 1).First(&unpessimisticTask)
	fmt.Printf("预期点赞数:50, 实际无锁并发点赞数:%d (惨遭更新丢失Bug!)\n\n", unpessimisticTask.Likes)

	fmt.Println("==== 实验2:使用GORM悲观锁(FOR UPDATE)修复=====")
	// reset
	db.Model(&Task{}).Where("id = ?", 1).Update("likes", 0)

	for i := 0; i < concurrentCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// 正确做法:事务 + FOR UPDATE 排他锁
			db.Transaction(func(tx *gorm.DB) error {
				var t Task
				// 关键 ： Clauses 加 锁定子句
				if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", 1).First(&t).Error; err != nil {
					return err
				}
				t.Likes++
				return tx.Save(&t).Error
			})
		}()
	}
	wg.Wait()

	var pessimisticTask Task
	db.Where("id = ?", 1).First(&pessimisticTask)
	fmt.Printf("预期点赞数: 50, 悲观锁修复后点赞数: %d (数据严格一致！)\n", pessimisticTask.Likes)

	fmt.Println("==== 实验3:使用乐观锁(CAS+版本号)修复")
	// reset点赞数和版本号
	db.Model(&Task{}).Where("id = ?", 1).Updates(map[string]interface{}{"likes": 0, "version": 0})

	for i := 0; i < concurrentCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// 乐观锁：不加FOR UPDATE, 靠“版本号条件 + 重试“保持一致性
			for attempt := 0; attempt < 1000; attempt++ { // 最多重试10次
				// 1 读（拿到当前版本号）
				var t Task
				if err := db.Where("id = ?", 1).First(&t).Error; err != nil {
					return
				}
				oldVersion := t.Version // 记下读到的版本号

				// 2 改（内存中+1）
				t.Likes++
				t.Version++

				// 3.写(核心：WHERE里带上版本号条件！)
				//   如果期间别人先改了这行，version已经变了，这个UPDATE影响行数为0
				result := db.Model(&Task{}).Where("id = ? AND version = ?", t.ID, oldVersion).
					Updates(map[string]any{
						"likes":   t.Likes,
						"version": t.Version,
					})
				if result.Error != nil {
					return
				}
				if result.RowsAffected == 1 {
					break // 更新成功，退出重试
				}
				// ❌ RowsAffected == 0 → 版本冲突, 别人抢先改了, 重新读再试
			}
		}()
	}
	wg.Wait()

	var optimisticTask Task
	db.Where("id = ?", 1).First(&optimisticTask)
	fmt.Printf("预期点赞数: 50, 乐观锁修复后点赞数: %d (数据严格一致！)\n", optimisticTask.Likes)
}

// 针对<从根上理解MySQL> chapter 10

package main

import (
	"fmt"
	"math/rand"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// 物理表模型-对应single_table
type SingleTable struct {
	ID          int    `gorm:"primaryKey;autoIncrement"`
	Key1        string `gorm:"column:key1;type:varchar(100);index:idx_key1"`
	Key2        int    `gorm:"column:key2;uniqueIndex:idx_key2"`
	Key3        string `gorm:"column:key3;type:varchar(100);index:idx_key3"`
	KeyPart1    string `gorm:"column:key_part1;type:varchar(100);index:idx_key_part,priority:1"`
	KeyPart2    string `gorm:"column:key_part2;type:varchar(100);index:idx_key_part,priority:2"`
	KeyPart3    string `gorm:"column:key_part3;type:varchar(100);index:idx_key_part,priority:3"`
	CommonField string `gorm:"column:common_field;type:varchar(100)"`
}

func (SingleTable) TableName() string {
	return "single_table"
}

func main() {
	// 1.连接MySQL
	dsn := "root:geo_secret_123@tcp(127.0.0.1:3306)/mysql_learn?charset=utf8mb4&parseTime=True&loc=Local"
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		panic("连接失败:" + err.Error())
	}
	fmt.Println("连接MySQL成功")

	// 2.自动建表
	err = db.AutoMigrate(&SingleTable{})
	if err != nil {
		panic("自动建表失败:" + err.Error())
	}
	fmt.Println("自动建表成功")

	// 3.插入10000行数据
	rng := rand.New(rand.NewSource(time.Now().UnixNano())) // 每次重启都完全随机的随机数生成器
	batch := make([]SingleTable, 0, 1000)                  // 每批次1000条，分10批插入

	for i := 1; i <= 10000; i++ {
		batch = append(batch, SingleTable{
			Key1:        fmt.Sprintf("key1_%d", i),
			Key2:        i,
			Key3:        fmt.Sprintf("key3_%d", i),
			KeyPart1:    fmt.Sprintf("part1_%d", rng.Intn(1000)),
			KeyPart2:    fmt.Sprintf("part2_%d", rng.Intn(1000)),
			KeyPart3:    fmt.Sprintf("part3_%d", rng.Intn(1000)),
			CommonField: fmt.Sprintf("common_%d", rng.Intn(5000)),
		})

		if len(batch) == 1000 || i == 10000 {
			result := db.Create(batch)
			if result.Error != nil {
				panic("批量插入失败:" + result.Error.Error())
			}
			fmt.Printf("已插入%d行 \n", i)
			batch = batch[:0] // 清空batch
		}
	}

	fmt.Println("🎉 全部 10000 行数据插入完成！")
}

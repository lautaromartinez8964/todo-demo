package main

import (
	"fmt"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger" // 导入GORM官方日志包
)

type Task struct {
	gorm.Model
	TaskName  string  `gorm:"type:varchar(255);not null;index"`
	TaskType  string  `gorm:"type:varchar(255);not null"`
	Latitude  float64 `gorm:"not null"`
	Longitude float64 `gorm:"not null"`
	Status    bool    `gorm:"default:false"` // 已完成/未完成
}

func main() {
	dsn := "root:123456@tcp(127.0.0.1:3306)/todo_db?charset=utf8mb4&parseTime=True&loc=Local"

	// 1.初始化数据库, 并开启详细的SQL日志输出
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		// 核心:打开Info级别的日志，GORM会把底层翻译出的每一行SQL实时打印在控制台
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		panic("数据库连接失败")
	}

	// 清空表重建，保证每次实验数据都干净
	db.Migrator().DropTable(&Task{})
	db.AutoMigrate(&Task{})

	fmt.Println("\n--- GORM Hign-end CRUD Experiment---")

	// ==== 场景1：批量插入（Create) ===
	fmt.Println("\n[Scene 1]Batch Insert 5 RS Tasks ")
	batchTasks := []Task{
		{TaskName: "Beijing Daxing airport", TaskType: "Target Detection", Latitude: 39.5, Longitude: 116.4, Status: true},
		{TaskName: "Valencia Flood", TaskType: "Change Detection", Latitude: 38.43, Longitude: -1.9, Status: true},
		{TaskName: "Xuzhou Soil Moisture", TaskType: "Quantitative Inversion", Latitude: 36.98, Longitude: 117.5, Status: false},
		{TaskName: "Xinjiang Gobi", TaskType: "Instance Segmentation", Latitude: 42.4, Longitude: 90.2, Status: false},
		{TaskName: "Yangshan Harbour", TaskType: "Target Detection", Latitude: 30.86, Longitude: 121.87, Status: true},
		{TaskName: "Yulin Mine Carbon Emissions", TaskType: "Quatitative Inversion", Latitude: 38.23, Longitude: 109.72, Status: false},
	}

	err = db.Transaction(func(tx *gorm.DB) error {
		if err := tx.CreateInBatches(&batchTasks, 2).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		fmt.Printf("[ERROR]Batch write transactions failed:%v/n", err)
	} else {
		fmt.Printf("[ERROR]Successfullt Commit Transaction Batchly!")
	}

	// ==== 场景2: 高级条件分页查询(Read) ====
	fmt.Println("\n[Scene 2]search for any Target Detection Tasks, in reverse created_at chronological order...")

	var pageTasks []Task
	pageSize := 2
	page := 1
	offset := (page - 1) * pageSize

	// 链式调用:指定特定列 + 条件过滤 + 排序 + 分页限制[2]
	db.Select("id", "task_name", "task_type", "status").
		Where("task_type = ?", "Target Detection").
		Where("status = ?", true).
		Order("created_at DESC").
		Limit(pageSize).
		Offset(offset).
		Find(&pageTasks)

	fmt.Printf("查询到第%d页数据(共%d条):\n", page, len(pageTasks))
	for _, t := range pageTasks {
		fmt.Printf("-ID: %d | 名字: %s | 类型: %s | 状态: %t", t.ID, t.TaskName, t.TaskType, t.Status)
	}

	// ==== 场景三:0值更新深坑与拯救 ====
	// GORM认为，如果一个字段是他的0值，（比如false, 0, ""),会直接忽略
	// 因此Update的时候如果想把Status从true改回bool，不能直接更改 task.Status = false
	if len(pageTasks) > 0 {
		targetTask := pageTasks[0]

		// ❌ 错误示范（如果你直接用 struct 更新，Status = false 会被 GORM 默默忽略）
		// targetTask.Status = false                           		db.Model(&targetTask).Updates(targetTask)

		// ✅ 工业级拯救法：使用 map 强制更新
		db.Model(&targetTask).Updates(map[string]any{
			"status": false,
		})

		fmt.Println(" [OK] Status is forced to false")
	}

	// ==== 场景4：物理删除(Unscoped Delete) ====
	fmt.Println("\n[Scene 4]Physically Clear Data")
	// 默认的db.Delete只是软删除
	// 如果我们要彻底从物理硬盘上把这几条脏数据删除
	// .Unscoped()是执行物理硬删除函数
	// .Where("l = l") 提供恒真条件， 绕过GORM的裸删保护
	err = db.Transaction(func(tx *gorm.DB) error {
		// 事务直接返回Error
		return tx.Unscoped().Where("1 = 1").Delete(&Task{}).Error
	})
	if err == nil {
		fmt.Println("   [OK] 实验数据物理擦除完毕！")
	} else {
		fmt.Printf("删除失败:%v\n", err)
	}
}

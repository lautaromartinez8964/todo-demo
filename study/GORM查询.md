在 Go 语言开发中，查询（Retrieve）是数据库操作中最繁琐、但也是最考验基本功的部分。

你觉得查询这一块有点陌生是完全正常的。GORM 的查询设计非常精妙，它采用的是**“链式调用（Method Chaining）”**的设计模式。

为了让你彻底看清、看懂、学会各种查询，今天我为你准备了一份**“GORM 7大核心查询场景全图谱”**。

我们不用抽象的概念，直接用我们之前的 **`Task`（遥感图像检测任务）** 作为统一的模型，把工业界最常用的 7 种查询场景逐一拆解。

---

## 💡 核心心法：链式方法（Chainable）与 立即方法（Finisher）

在学习具体查询前，你必须记住一句话：**GORM 的方法分为两种，只有“立即方法”才会真正向数据库发送 SQL 报文！**

*   **链式方法（只拼装，不执行）**：
    如 `Where()`、`Select()`、`Order()`、`Limit()`。
    这些方法只是在内存里“搭积木”拼装 SQL，它们返回的依然是 `*gorm.DB` 对象，不会产生网络 I/O。
*   **立即方法（终结者，立刻执行）**：
    如 `First()`、`Find()`、`Count()`、`Pluck()`、`Scan()`。
    这些方法是“句号”。一旦调用，GORM 会立刻把拼好的 SQL 发给 MySQL 执行。

---

## 🛠️ GORM 7 大核心查询场景代码与 SQL 对比

以下所有场景，我们均假设数据库中已经存在多条遥感任务记录。

### 场景 1：精准主键查询（单条数据）
*   **需求**：已知任务 ID = 3，我们要获取这个任务的完整详情。

```go
var task Task

// 方式 A：First（按主键升序，拿第一条。最推荐，带自动 LIMIT 1）
err := db.First(&task, 3).Error
// 💻 底层 SQL: SELECT * FROM tasks WHERE id = 3 AND deleted_at IS NULL ORDER BY id LIMIT 1;

// 方式 B：Take（随机拿一条，不进行任何排序，性能略好于 First）
err := db.Take(&task, 3).Error
// 💻 底层 SQL: SELECT * FROM tasks WHERE id = 3 AND deleted_at IS NULL LIMIT 1;

// 方式 C：Last（按主键降序，拿最后一条）
err := db.Last(&task, 3).Error
// 💻 底层 SQL: SELECT * FROM tasks WHERE id = 3 AND deleted_at IS NULL ORDER BY id DESC LIMIT 1;
```

---

### 场景 2：基础条件查询（防注入）
*   **需求**：查询所有算法类型为 `"YOLO_V8"` 的任务。

```go
var tasks []Task

// 使用占位符 ? 传递参数，安全防 SQL 注入
err := db.Where("task_type = ?", "YOLO_V8").Find(&tasks).Error

// 💻 底层 SQL: SELECT * FROM tasks WHERE task_type = 'YOLO_V8' AND deleted_at IS NULL;
```
*   *📌 补充（其他比较操作符）*：
    ```go
    // 不等于 (<>)
    db.Where("task_type <> ?", "YOLO_V8").Find(&tasks)
    // 大于 (>)
    db.Where("latitude > ?", 30.0).Find(&tasks)
    // 模糊查询 (LIKE) - 查找名字里带“机场”的任务
    db.Where("task_name LIKE ?", "%机场%").Find(&tasks)
    ```

---

### 场景 3：多条件组合查询（AND 与 OR）
*   **需求**：查询所有 `"YOLO_V8"` 算法，**且**状态为 `"已完成(true)"` **或者** 名字里包含 `"北京"` 的任务。

```go
var tasks []Task

// 链式写法的 Where 默认就是 AND 关系
db.Where("task_type = ?", "YOLO_V8").
   Where("status = ?", true).
   Or("task_name LIKE ?", "%北京%").
   Find(&tasks)

// 💻 底层 SQL: 
// SELECT * FROM tasks 
// WHERE (task_type = 'YOLO_V8' AND status = true) 
//    OR task_name LIKE '%北京%' 
//   AND deleted_at IS NULL;
```

---

### 场景 4：特定列查询（避开 SELECT * 性能开销）
*   **需求**：只需要在地图上渲染任务的经纬度坐标，不需要提取任务名字、创建时间等大字段，节省带宽。

```go
type Coordinate struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}
var coords []Coordinate

// 使用 Select 指定提取列，利用 Scan 将结果注入到我们自定义的轻量级结构体中
db.Model(&Task{}).
   Select("latitude", "longitude").
   Scan(&coords)

// 💻 底层 SQL: SELECT latitude, longitude FROM tasks WHERE deleted_at IS NULL;
```
*   *💡 避坑提示*：如果用 `Scan` 接收数据，**必须**显式指定 `db.Model(&Task{})`，因为 `coords` 结构体并不是 GORM 模型，GORM 无法通过它推导出应该去查哪张表。

---

### 场景 5：排序与分页查询（列表展示）
*   **需求**：在后台管理页面中，展示最新创建的任务，每页展示 10 条，当前是第 2 页。

```go
var tasks []Task
pageSize := 10
page := 2
offset := (page - 1) * pageSize // 计算得出偏移量为 10

db.Order("created_at DESC"). // DESC: 降序(最新在前); ASC: 升序(最老在前)
   Limit(pageSize).
   Offset(offset).
   Find(&tasks)

// 💻 底层 SQL: 
// SELECT * FROM tasks 
// WHERE deleted_at IS NULL 
// ORDER BY created_at DESC 
// LIMIT 10 OFFSET 10;
```

---

### 场景 6：聚合统计查询（Count、Group、Having）
*   **需求**：统计已经完成（`status = true`）的任务总数。

```go
var count int64 // ⚠️ 必须是 int64 类型

db.Model(&Task{}).
   Where("status = ?", true).
   Count(&count) // Count 会把统计出的数字写进 count 变量

fmt.Printf("已完成的任务总数: %d\n", count)
// 💻 底层 SQL: SELECT count(*) FROM tasks WHERE status = true AND deleted_at IS NULL;
```

*   **进阶需求**：按“算法类型”分组（Group），统计每种算法各自有多少个任务，且只保留任务数超过 1 个的分组（Having）：
```go
type GroupResult struct {
	TaskType string
	Total    int64
}
var results []GroupResult

db.Model(&Task{}).
   Select("task_type", "count(*) as total").
   Group("task_type").
   Having("total > ?", 1).
   Scan(&results)

// 💻 底层 SQL: 
// SELECT task_type, count(*) as total 
// FROM tasks 
// WHERE deleted_at IS NULL 
// GROUP BY task_type 
// HAVING total > 1;
```

---

### 场景 7：单列切片提取（Pluck）
*   **需求**：提取数据库中所有不重复的算法类型列表（下拉菜单渲染）。

```go
var algos []string

// Distinct() 自动去重
db.Model(&Task{}).
   Distinct().
   Pluck("task_type", &algos) 

fmt.Println("数据库中的算法列表:", algos) // 输出: [YOLO_V8 SAM U-Net]
// 💻 底层 SQL: SELECT DISTINCT task_type FROM tasks WHERE deleted_at IS NULL;
```

---

## 🧪 专项小实验：把上面的代码跑起来！

光看不练是学不会查询的。我们现在就把它们融入到一个完整的 Go 程序中。

请在你的 WSL 里，用以下代码**完全覆盖** `main.go`。你可以逐个打开代码里的注释，点击运行，亲眼看看控制台打印出的各种 SQL 语句：

```go
package main

import (
	"fmt"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type Task struct {
	gorm.Model
	TaskName  string  `gorm:"type:varchar(255);not null"`
	TaskType  string  `gorm:"type:varchar(255);not null"`
	Latitude  float64 `gorm:"not null"`
	Longitude float64 `gorm:"not null"`
	Status    bool    `gorm:"default:false"`
}

func main() {
	dsn := "root:123456@tcp(127.0.0.1:3306)/todo_db?charset=utf8mb4&parseTime=True&loc=Local"
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info), // 打印详细 SQL 日志
	})
	if err != nil {
		panic(err)
	}

	// 1. 重置并初始化模拟数据
	db.Migrator().DropTable(&Task{})
	db.AutoMigrate(&Task{})

	db.Create(&[]Task{
		{TaskName: "北京大兴机场检测", TaskType: "YOLO_V8", Latitude: 39.5, Longitude: 116.4, Status: true},
		{TaskName: "上海高架车辆计数", TaskType: "YOLO_V8", Latitude: 31.2, Longitude: 121.4, Status: false},
		{TaskName: "深圳盐田港口监测", TaskType: "SAM", Latitude: 22.5, Longitude: 114.0, Status: false},
		{TaskName: "广州水稻面积估产", TaskType: "U-Net", Latitude: 23.1, Longitude: 113.2, Status: true},
		{TaskName: "北京二环拥堵分析", TaskType: "YOLO_V8", Latitude: 39.9, Longitude: 116.3, Status: false},
	})

	fmt.Println("\n--- 🏁 开始测试我们的查询小实验 ---")

	// -------------------- 实验 1: 组合条件查询 --------------------
	fmt.Println("\n🔎 [实验 1] 查找北京地区 且 算法为 YOLO_V8 的任务:")
	var list1 []Task
	db.Where("task_name LIKE ?", "%北京%").
		Where("task_type = ?", "YOLO_V8").
		Find(&list1)
	for _, t := range list1 {
		fmt.Printf("   - 找到任务: %s (%s)\n", t.TaskName, t.TaskType)
	}

	// -------------------- 实验 2: 统计 YOLO_V8 算法个数 --------------------
	fmt.Println("\n📊 [实验 2] 统计 YOLO_V8 算法的任务总数:")
	var count int64
	db.Model(&Task{}).Where("task_type = ?", "YOLO_V8").Count(&count)
	fmt.Printf("   - YOLO_V8 任务数: %d\n", count)

	// -------------------- 实验 3: Pluck 提取所有任务名字 --------------------
	fmt.Println("\n🏷️ [实验 3] 提取所有任务的名称列表:")
	var names []string
	db.Model(&Task{}).Pluck("task_name", &names)
	fmt.Printf("   - 任务名称列表: %v\n", names)
}
```

---

## 🎯 你的今日挑战

请在上面的 `main.go` 中，**在 `fmt.Println("\n--- 开始测试我们的查询小实验 ---")` 下方**，新加一段你的查询代码。

### 🔴 挑战目标：写一个“空间过滤加排序”的复合查询
*   **筛选条件 1**：只筛选 **纬度（`Latitude`）大于 `30.0`** 的任务。
*   **筛选条件 2**：只筛选 **状态（`Status`）为 `false`** 的任务。
*   **排序规则**：按照 **ID 降序（`id DESC`）** 排列。
*   **限制数量**：只要最符合条件的前 **1** 个。
*   **实现方式**：
    1.  声明 `var myTask Task`。
    2.  用**一行链式调用代码**（组合 `Where`、`Order`、`Limit` 或 `First`）查出结果。
    3.  打印出这个任务的名字和纬度。
    4.  **把你的查询代码，以及 GORM 打印在终端里的对应 SQL 发给我。**

看完了这一套，你对查询的脉络是否清晰了一些？动手试一下，我在这里等你提交代码！🚀
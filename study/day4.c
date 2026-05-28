package main

import (
	"fmt"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// ====1.定义数据库Model====

type Todo struct {
	// gorm.Model 内部包含了: ID, CreateAt, UpdatedAt, DeleteAt 四个字段
	gorm.Model
	Title  string `gorm:"type:varchar(255);not null"` // 使用varchar显示控制字段长度，防止浪费MYSQL存储空间
	Status bool   `gorm:"default:false"`              // 设置数据库层面的默认值
}

type User struct {
	gorm.Model
	Username string `gorm:"type:varchar(255);not null;uniqueindex"`
	// 一对多关联:一个User可以拥有多个Task
	// foreginKey:UserID 显式指定Task表里的UserID字段作为外键
	// 即：Task表里的UserID字段是User表里的主键，必须在User表里存在
	Tasks []Task `gorm:"foreignKey:UserID"`
}

// day4任务：将摇杆任务模型写入mysql数据库
type Task struct {
	gorm.Model
	TaskName  string  `gorm:"type:varchar(255);not null;index"`
	TaskType  string  `gorm:"type:varchar(255);not null"`
	Latitude  float64 `gorm:"not null"` // 标签是强行指定go->数据库类型的，数据库type没有float64只有float，不用写type让gorm直接翻译就可以
	Longitude float64 `gorm:"not null"`
	UserID    uint    // 外键字段，对应User的ID
}

// 初始化应用数据库连接，配置连接池并自动迁移建表。
func main() {
	// 整体流程:配置DSN → 建立连接 → 配置连接池 → 自动迁移建表

	// ==== 2. 配置DSN ===
	dsn := "root:123456@tcp(127.0.0.1:3306)/todo_db?charset=utf8mb4&parseTime=True&loc=Local"

	// ==== 3.开启数据库连接 ====
	// 两阶段构造：先用mysql.Open(dsn)创建底层驱动连接器，再传给gorm.Open()包裹成ORM实例
	// 这种分离让gorm可以支持postgreSQL, SQLite等多种数据库驱动
	// gorm.Config{}里包含了很多高级配置，比如控制是否在后台输出SQL日志等，此处留空使用默认配置
	db, err := gorm.Open(mysql.Open((dsn)), &gorm.Config{})
	if err != nil {
		panic("CRITICAL:MySQL database connection failed, plz check if docker open,details: " + err.Error())
	}
	fmt.Println("[ok]MySQL database TCP route connection success!")

	// ==== 4.配置高性能连接池 ====
	sqlDB, err := db.DB() // 从GORM包装器中提取底层的*sql.DB实例
	if err != nil {
		panic("CRITICAL:Access botom connection pool failed:" + err.Error())
	}

	sqlDB.SetMaxIdleConns(10)                 // 限制空闲连接池最大连接数为10（默认2
	sqlDB.SetMaxOpenConns(100)                // 限制Go在同一时刻最多允许和MySQL保持的的连接数为100（防止撑爆MySQL连接上限，默认无限制)
	sqlDB.SetConnMaxLifetime(5 * time.Minute) // 五分钟后强制销毁空闲长连接，防止因网络波动导致僵尸连接（默认永不过期）

	// ==== 5.自动迁移建表(AutoMigrate) ====
	// AutoMigrate
	// 作用：基于传入的一个或多个结构体的定义，自动创建/更新表结构（新增字段/索引等），不会删除已有列或修改列类型
	// 思考:GORM是怎么知道数据库里有没有这张表的？
	// 答: GORM会在底层自动去查询MySQL官方的元数据视图
	//   "SELECT * FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_NAME = 'todos"
	//   如果查不到，他就会默默执行DDL建表语句

	// 同时迁移Todo和Task结构体
	err = db.AutoMigrate(&User{}, &Task{})
	if err != nil {
		panic("CRITICAL: 数据表自动迁移失败: " + err.Error())
	}
	fmt.Println("✨ [OK] GORM 元数据分析完毕，多表关联自动迁移成功！！")

	//  事务控制(Transaction)挑战
	// 需求:我们要在事务里原子性地创建：1个用户+2个任务
	// db.Transaction()是GORM封装的事务闭包方法
	// 传入的回调函数接受一个gorm.DB实例参数tx，这个tx是事务上下文里的数据库连接，所有在这个函数里对tx的操作都会被包含在同一个事务里
	// 回调函数的返回值决定事务命运:return nil -> Commit, return error -> Rollback
	err = db.Transaction(func(tx *gorm.DB) error {
		// 1.在事务里创建用户
		newUser := User{Username: "musiala"}
		if err := tx.Create(&newUser).Error; err != nil {
			return err // 返回err, GORM会自动触发Rollback（回滚）
		}
		fmt.Printf("[Transaction]User create successfully, auto generate ID is %d\n", newUser.ID)

		// 2.在事务中创建第一个遥感任务， 将其UserID绑定为刚才生成的newUser.ID
		task1 := Task{
			TaskName:  "daxing airport satellite recogonition",
			TaskType:  "YOLOv8",
			Latitude:  39.50,
			Longitude: 116.41,
			UserID:    newUser.ID, //绑定外键,一对多的关系
		}
		if err := tx.Create(&task1).Error; err != nil {
			return err
		}

		// 3.在事务中创建第二个遥感任务
		task2 := Task{
			TaskName:  "xuzhou soil moisture monitoring",
			TaskType:  "Quantitative Inversion",
			Latitude:  36.50,
			Longitude: 117.41,
			UserID:    newUser.ID, // 绑定外键k
		}
		if err := tx.Create(&task2).Error; err != nil {
			return err
		}

		// 🔥 人为制造失败测试点
		// return fmt.Error("simulate halfway elapse, forcefully rollback")
		// 你会发现，即使上面User创建成功的log打印了，但去数据库里看，不管是User还是Task都没有写入成功，这就是事务的威力
		return nil
	})
	if err != nil {
		fmt.Println("事务执行失败，数据安全回滚：", err.Error())
	} else {
		fmt.Println("transaction submit success!user and two tasks have been writen to database safely.")
	}

	// 关联预加载查询(Preload)挑战
	// 需求:我们需要查询刚才创建的用户，并顺便查出他名下所有的遥感任务
	// 拒绝N+1查询，必须使用preload
	var foundUser User

	// 请在这里写出一行GORM预加载查询代码，根据名字musiala_rs查询用户，并预加载Tasks
	db.Preload("Tasks").Where("username = ?", "musiala").First(&foundUser)

	// 打印查询结果验证
	fmt.Printf("\n🔎 关联查询结果:\n")
	fmt.Printf("用户名: %s (注册时间: %s)\n", foundUser.Username, foundUser.CreatedAt.Format("2006-01-02 15:04:05"))
	fmt.Printf("名下遥感任务数量: %d\n", len(foundUser.Tasks))
	for i, task := range foundUser.Tasks {
		fmt.Printf("  └─ 任务 [%d]: %s (算法: %s, 坐标: %.2f, %.2f)\n",
			i+1, task.TaskName, task.TaskType, task.Latitude, task.Longitude)
	}
}

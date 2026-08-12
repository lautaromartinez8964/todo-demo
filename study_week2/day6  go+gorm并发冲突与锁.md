收到。我们暂停进入 Day 7，**把 Day 6 从最基础的“Go 内存 vs MySQL 磁盘”物理动作开始**，一步步推导 Go 语言与 MySQL 数据库在并发写数据时的物理过程。

这篇精讲不假设你懂任何框架魔法，完全立足于你已掌握的 **Go 变量内存**、**Goroutine 协程** 和 **MySQL 锁机制**，把“更新丢失”、“悲观锁”与“乐观锁 CAS”讲透。

---

# 📚 Day 6 细致重讲：Go + GORM 并发写冲突与锁选型物理全解析

---

## 🗺️ Day 6 知识架构图

```text
[ 1. 物理动作拆解 ] ──► 一行 Go 代码在 CPU 内存与磁盘上的真实旅程
                             │
                             ▼
[ 2. 事故现场复现 ] ──► 为什么 50 个协程会导致“更新丢失 (Lost Update)”？
                             │
                             ▼
[ 3. 解决方案 A ] ──► 悲观锁 (Pessimistic Lock)
                             ├──► 原生 SQL：SELECT ... FOR UPDATE (Day 5 的 Record Lock)
                             └──► GORM 翻译：tx.Clauses(clause.Locking{Strength: "UPDATE"})
                             │
                             ▼
[ 4. 解决方案 B ] ──► 乐观锁 (Optimistic Lock)
                             ├──► 核心概念：CAS (Compare And Swap, 比较并交换)
                             └──► 物理实现：WHERE id = X AND version = V
                             │
                             ▼
[ 5. Git 变基线 ] ──► git rebase -i 的提交链条压缩
```

---

## 第一章：认知基石 —— 一行 Go 代码的物理旅程

要搞懂并发 Bug，首先必须在脑海中建立 **Go 程序（CPU / 内存）** 与 **MySQL 数据库（磁盘 / Buffer Pool）** 的物理分离模型。

它们是运行在不同进程、甚至是不同服务器上的两个独立个体：

```text
+------------------------------------+           TCP 网络二进制包           +------------------------------------+
|         Go 应用程序 (RAM 内存)      | ─────────────────────────────────► |         MySQL 数据库 (磁盘/内存页)   |
|                                    |                                    |                                    |
| 1. 定义变量 task (内存占几百字节)   | ◄───────────────────────────────── | 2. 表 tasks 存放在磁盘 .ibd 文件中  |
+------------------------------------+                                    +---------------------------------- --+
```

### 当你在 Go 代码里写下点赞逻辑时，物理上发生了 3 个步骤：

```go
// 步骤 1：从 MySQL 查出数据
var task Task
db.Where("id = ?", 1).First(&task)

// 步骤 2：在 Go 的内存里自增 1
task.Likes = task.Likes + 1

// 步骤 3：把修改后的结果写回 MySQL
db.Save(&task)
```

我们来逐行拆解这 3 行代码在物理硬件上做了什么：

1.  **执行 `db.First(&task)`**：
    *   Go 通过 TCP 网络向 MySQL 发送一条 SQL：`SELECT * FROM tasks WHERE id = 1 LIMIT 1;`。
    *   MySQL 读取磁盘/内存页，把 `likes = 10` 打包成网络数据包发回给 Go。
    *   Go 把这行数据解析成自己内存里的一个 `struct` 变量 `task`。此时 Go 内存里的 `task.Likes` 等于 **10**。
2.  **执行 `task.Likes = task.Likes + 1`**：
    *   **极其关键物理点**：这一步**完全发生在 Go 程序的 CPU 寄存器和 RAM 内存里**！
    *   Go 内存里的 `task.Likes` 变成了 **11**。
    *   **此时 MySQL 数据库对此一无所知！** MySQL 磁盘里的 `likes` 依然是 **10**！
3.  **执行 `db.Save(&task)`**：
    *   Go 再次通过 TCP 网络向 MySQL 发送一条 SQL：`UPDATE tasks SET likes = 11 WHERE id = 1;`。
    *   MySQL 收到指令，把磁盘/内存页里的 `likes` 改成了 **11**。

---

## 第二章：致命事故 —— 为什么并发会导致“更新丢失（Lost Update）”？

知道了单线程的物理旅程，现在假设有 **两个 Go 协程（Goroutine A 和 Goroutine B）** 同时在执行上面的点赞逻辑。

我们把时间线拉开，看看硬件层面上发生了什么冲突：

```text
时间线      Goroutine A (内存)             Goroutine B (内存)            MySQL 数据库 (磁盘/Buffer Pool)
 ──────────────────────────────────────────────────────────────────────────────────────────
 T1       查到 id=1, likes=10  ───────────► 查到 id=1, likes=10  ──────────► 磁盘 likes 当前值 = 10
 
 T2       内存计算: 10 + 1 = 11          内存计算: 10 + 1 = 11          磁盘 likes 当前值 = 10
 
 T3       发送 UPDATE SET likes=11 ─────────────────────────────────────► 磁盘 likes 被改成了 11
 
 T4                                      发送 UPDATE SET likes=11 ──────► 磁盘 likes 再次被改成了 11 ！
```

### 物理灾难总结：
1.  **T1 时刻**：Goroutine A 和 B 都读到了初始值 `10`。
2.  **T2 时刻**：A 和 B 都在各自独立的 CPU 内存里，算出了 `11`。
3.  **T3 时刻**：Goroutine A 先把 `likes = 11` 写进了 MySQL。
4.  **T4 时刻**：Goroutine B 随后也把 `likes = 11` 写进了 MySQL！
5.  **结果**：A 和 B 两个用户明明各点了 1 次赞（应该一共加 2 次，变成 `12`），但最终数据库里的数值变成了 `11`！**Goroutine A 的点赞结果被 Goroutine B 给强制覆盖掉了！**

这就是典型的 **更新丢失（Lost Update / 写覆盖）**。如果有 50 个协程并发点赞，绝大部分写操作都会重复覆盖，最终点赞数可能只有 2 或 3。

---

## 第三章：解决方案 1 —— 悲观锁（Pessimistic Locking）

为了防止被别人覆盖，最直接的思路是：**“我不相信任何人，在我读数据时，我就把数据库这行锁死，不准任何人读和写！”**

这就是 **悲观锁（Pessimistic Locking）**。

---

### 1. 悲观锁的物理本质
悲观锁并不是 Go 语言创造的概念，它**直接依赖我们在 Day 5 学到的 MySQL 行锁（`SELECT ... FOR UPDATE`）**！

当 Goroutine A 执行 `SELECT * FROM tasks WHERE id = 1 FOR UPDATE;` 时：
*   MySQL 会立刻在聚簇索引 B+ 树的 `id = 1` 节点上，挂上一把 **记录锁（Record Lock）** [2]。
*   当 Goroutine B 尝试执行同样的 `SELECT ... FOR UPDATE` 时，**MySQL 会强制将 Goroutine B 阻塞挂起（Block）**，必须等 Goroutine A 提交事务（`COMMIT`）释放锁后，B 才能继续 [2]！

---

### 2. 在 GORM 中怎么写？（逐行代码翻译）

GORM 只是对原生 SQL 的一层封装。我们需要用 GORM 的 `Clause` 语法告诉它：“请帮我生成带 `FOR UPDATE` 的 SQL”。

```go
// 注意：悲观锁必须运行在事务（Transaction）中，因为只有事务提交（COMMIT）时锁才会释放！
db.Transaction(func(tx *gorm.DB) error {
    var task Task
    
    // GORM 代码：
    // tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", 1).First(&task)
    // 
    // 翻译成原生 SQL 就是：
    // SELECT * FROM tasks WHERE id = 1 FOR UPDATE;
    if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", 1).First(&task).Error; err != nil {
        return err // 查失败自动回滚
    }

    // 此时 MySQL 上的 id = 1 这行记录已经被我们独占锁定！
    // 任何其他尝试读取/修改该行的协程全都被 MySQL 阻塞在门外！
    task.Likes++

    // Save 会发送 UPDATE 语句
    // 闭包函数返回 nil 后，GORM 自动发送 COMMIT，释放 MySQL 行锁！
    return tx.Save(&task).Error
})
```

---

### 3. 加了悲观锁后的并发时间线：

```text
时间线      Goroutine A                     Goroutine B                     MySQL 数据库
 ──────────────────────────────────────────────────────────────────────────────────────────
 T1       发送 SELECT ... FOR UPDATE ─────────────────────────────────────► 锁定 id=1 ( Record Lock )
 
 T2       读到 likes = 10                发送 SELECT ... FOR UPDATE ───► 被 MySQL 阻塞！进入等待...
 
 T3       内存计算 10+1=11
 
 T4       UPDATE likes=11 & COMMIT ───────────────────────────────────────► 数据变为 11，释放行锁！
                                                                          │
 T5                                      被解除阻塞，读取最新值 11 ◄──────┘
 
 T6                                      内存计算 11+1=12
 
 T7                                      UPDATE likes=12 & COMMIT ───────► 数据变为 12！(绝对准确)
```

---

## 第四章：解决方案 2 —— 乐观锁与 CAS 物理拆解

悲观锁虽然安全，但缺点也很明显：**如果并发很高，大量协程都在 MySQL 门外阻塞排队，系统的吞吐量会急剧下降。**

如果我们不想在 MySQL 里加锁，还能防住写覆盖吗？可以！这就是 **乐观锁（Optimistic Locking）**。

---

### 1. 什么是 CAS 机制？（Compare And Swap，比较并交换）

乐观锁的核心思想是：“假定大家很少发生冲突，所以我读取数据时**绝不给数据库加锁**。但是在最后写回数据库时，我要先**比较（Compare）** 一下数据有没有被别人动过，没动过我才**交换写回（Swap）**”。

在数据库中实现 CAS，通常需要给表增加一列：**`version`（版本号，整数）**。

---

### 2. 乐观锁的表结构设计与 SQL 物理逻辑

假设我们的表定义如下：

| id | title | likes | version |
| :---: | :---: | :---: | :---: |
| 1 | Landsat8 分析 | 10 | **1** |

当两个协程 Goroutine A 和 B 尝试用乐观锁更新时：

#### 步骤 1：快照读取（不加任何锁）
*   Goroutine A 查到：`likes = 10, version = 1`。
*   Goroutine B 也查到：`likes = 10, version = 1`。

#### 步骤 2：Go 内存计算
*   Goroutine A 算出的新值为：`likes = 11, new_version = 2`。
*   Goroutine B 算出的新值为：`likes = 11, new_version = 2`。

#### 步骤 3：CAS 比较并写入（核心点！）
写入时，我们**不直接写回**，而是在 SQL 的 `WHERE` 条件里带上我们刚才读到的旧 `version`：

Goroutine A 先发送 SQL：
```sql
UPDATE tasks 
SET likes = 11, version = 2 
WHERE id = 1 AND version = 1;  -- 关键：限制只有旧 version 等于 1 时才允许修改！
```

*   MySQL 执行检查：当前 `id = 1` 的行，`version` 确实等于 `1`！
*   MySQL 执行更新，把 `likes` 改为 `11`，把 `version` 改为 `2`！
*   MySQL 返回：**受影响行数 `RowsAffected = 1`**（代表更新成功）。

紧接着，Goroutine B 也发送同样的 SQL：
```sql
UPDATE tasks 
SET likes = 11, version = 2 
WHERE id = 1 AND version = 1;  -- 它的 WHERE 条件里依然要求的旧 version 是 1
```

*   MySQL 执行检查：当前 `id = 1` 的行，`version` 已经被 Goroutine A 改成了 `2`！
*   MySQL 发现 `WHERE version = 1` 匹配不到任何行！
*   **MySQL 放弃更新，返回：受影响行数 `RowsAffected = 0`！**

---

### 3. Go 语言如何感知乐观锁失败？

在 Go 代码中，我们可以通过 GORM 提供的 `RowsAffected` 属性，判断刚才的 CAS 语句有没有写成功：

```go
// 1. 读取当前数据（不用 FOR UPDATE，不加锁）
var task Task
db.Where("id = ?", 1).First(&task) // 假设读出 task.Version = 1

// 2. 尝试 CAS 更新
result := db.Model(&Task{}).
    Where("id = ? AND version = ?", task.ID, task.Version). // 限制 version 必须依然是 1
    Updates(map[string]interface{}{
        "likes":   task.Likes + 1,
        "version": task.Version + 1, // 版本号自增 1
    })

// 3. 检查受影响行数
if result.RowsAffected == 0 {
    // 报错！说明在步骤 1 到 步骤 2 之间，有其他人抢先修改了数据，导致 version 发生了变化！
    // 此时可以进行“重试”：重新读取最新的 version，重新计算，重新发送 UPDATE。
}
```

---

## 第五章：悲观锁 vs. 乐观锁 选型总结

| 对比维度 | 悲观锁（Pessimistic Lock） | 乐观锁（Optimistic Lock） |
| :--- | :--- | :--- |
| **底层原理** | 依靠 MySQL 行锁（`SELECT ... FOR UPDATE`） | 依靠应用层 CAS 机制（`WHERE version = V`） |
| **加锁时机** | 读数据时就强行锁住物理行 | 读数据不加锁，只有最终写回时检查版本 |
| **高并发性能** | 锁竞争剧烈，协程容易阻塞，TPS 较低 | 无 DB 锁开销，高并发读性能极高 |
| **适用场景** | **写多读少、并发竞争极高**（例如：商品秒杀、限量抢票） | **读多写少、并发冲突较低**（例如：用户信息修改、文章编辑） |

---

## 第六章：Git 交互式变基（`git rebase -i`）物理链条

在 Git 中，每一次提交（Commit）都是物理上相连的一个**链表节点**：

```text
[ Commit 1: 初始化工程 ] ◄─ [ Commit 2: 写了点代码 ] ◄─ [ Commit 3: 修复拼写错误 ] ◄─ [ Commit 4: 又修复错误 ]
```

如果你要把代码合并到主干分支（`main`），提交树上有大量形如 `fix typo` 的废垃圾提交，会被团队拒绝。

我们需要用 `git rebase -i HEAD~3`（意为：交互式整理最近 3 次提交），将 `Commit 2`、`3`、`4` **压缩合并（Squash）** 为一个干净的提交：

### 命令行操作步骤：
1. 运行命令：
   ```bash
   git rebase -i HEAD~3
   ```
2. 终端会自动打开文本编辑器，列出最近 3 次提交：
   ```text
   pick a1b2c3d 写了点代码
   pick e4f5g6h 修复拼写错误
   pick i7j8k9l 又修复错误
   ```
3. 将第 2、3 行开头的 `pick` 改成 `s`（`s` 代表 `squash`，合并入上一个提交）：
   ```text
   pick a1b2c3d 写了点代码
   s e4f5g6h 修复拼写错误
   s i7j8k9l 又修复错误
   ```
4. 保存并退出编辑器，Git 会让你重新写一条干净的 Commit 注释，提交树立刻变成了干净的一条线：

```text
[ Commit 1: 初始化工程 ] ◄─ [ Commit 2 (融合版): 完整开发点赞功能 ]
```

---

## 💻 第七章：Go 代码实操指南（请在 VS Code 中运行）

我们在 Day 6 提供的 `main.go` 代码中，包含两个实验：
1. **实验 1**：启动 50 个协程，不加任何锁进行点赞。你会看到点赞数惨遭写覆盖，最终结果远小于 50。
2. **实验 2**：启动 50 个协程，使用 GORM 悲观锁（`clause.Locking{Strength: "UPDATE"}`）。你会看到点赞数严格等于 50。

如果你在本地 VS Code 运行该代码，请确保你的 `geo_mysql` 容器正在运行（端口 3306），然后执行：

```bash
go run main.go
```

---

## 🏁 Day 6 巩固作业（请回答并在回复中提交）

请根据今天重新精讲的物理动作，回答以下 3 道题目：
                                                                                
1. **写覆盖物理过程解析**：
   * 在不加锁的情况下，当 Goroutine A 和 Goroutine B 同时读取到了 `likes = 10` 时，为什么它们最终各自执行 `Likes++` 并 `db.Save()` 后，数据库里的 `likes` 是 `11` 而不是 `12`？请用“Go 内存与磁盘更新”的过程解释。

2. **悲观锁与乐观锁区别**：
   * GORM 代码 `tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&task)` 在物理上向 MySQL 发送了哪条带锁的 SQL 语句？
   * 在乐观锁中，如果一条 SQL `UPDATE tasks SET likes=11, version=2 WHERE id=1 AND version=1;` 返回的受影响行数 `RowsAffected` 等于 `0`，代表在物理上发生了什么？

3. **Git 变基命令**：
   * 如果我们想将最近的 4 次提交压缩合并为一个干净的提交，应该输入哪条 Git 命令？在弹出的编辑器中，需要将后 3 次提交开头的 `pick` 单词改成什么字母？

请完成这三道题目并在回复中提交。收到后，我会附上 **Day 6 标准答案**，并带领你进入 **Day 7：15 道 MySQL 底层深水区真题考核与周总结**！
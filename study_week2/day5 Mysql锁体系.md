收到你的反馈。之前的讲解在**“锁到底加在哪里、为什么会退化”**这两个物理细节上讲得太快，导致你听起来感到脱节和模糊。

今天我们**重构 Day 5**。我不假设你懂任何新概念，完全从你已经掌握的 **“B+ 树叶子节点结构”** 和 **“快照读 vs 当前读”** 出发，用最细致的物理推导，把 MySQL 的锁体系彻底讲透！

---

# 📚 Day 5 细致重讲：MySQL 锁体系的物理真相与降级推导

---

## 第一章：知识衔接 —— 从“快照读”到“当前读”

在进入“锁”之前，我们先回忆你已经掌握的知识：

### 1.1 什么是快照读？（你已经懂的）
你学过 MVCC：当执行普通的 `SELECT * FROM t WHERE id = 1;` 时，MySQL 会利用 `ReadView` 去读 `undo log` 里的历史快照 [2]。
*   **特点**：**不加任何锁**！写不阻塞读，读不阻塞写 [2]。

### 1.2 什么是当前读？（为什么必须引入“锁”？）
如果我们要执行以下操作：
*   `UPDATE account SET balance = balance - 100 WHERE id = 1;`
*   `DELETE FROM account WHERE id = 1;`
*   `SELECT * FROM account WHERE id = 1 FOR UPDATE;` （显式要求加写锁）

这些操作叫 **“当前读”**。
**当前读的定义**：必须读取磁盘/内存里**最新的、已提交的数据**，并且为了防止在读取和修改期间被别人插队修改，必须对这行数据进行 **“加锁（Locking）”**。

> 📌 **核心结论 1**：MVCC 解决的是“读-写”并发；而 **“锁”解决的是“写-写”并发**（防止两个人同时修改同一行数据造成覆盖）。

---

## 第二章：锁到底加在哪里？（核心物理真相）

很多初学者觉得“锁”是挂在内存里一个虚无缥缈的符号，这是错的！

> 📌 **核心结论 2：MySQL InnoDB 的锁，是直接加在“B+ 树叶子节点的索引记录”上的！**

```text
               B+ 树叶子节点物理页 (16KB)
+-------------------------------------------------------------+
| [ id = 1 ]  ──►  [ id = 5 (加锁标记🔒) ]  ──►  [ id = 10 ]   |
+-------------------------------------------------------------+
```

*   如果你的表有主键 `id`，锁就会挂在**聚簇索引 B+ 树**的 `id` 节点上。
*   如果你的表用二级索引 `idx_age` 查询，锁会先挂在**二级索引 B+ 树**上，再回表挂到**聚簇索引 B+ 树**上 [2]。
*   **重点**：如果一条 `UPDATE` 语句的 `WHERE` 条件**没有走任何索引**，MySQL 无法在 B+ 树上快速定位节点，**只能把聚簇索引树上所有的节点全部加锁**！这在物理上就变成了**全表锁（Table Lock）**！这就是为什么写 SQL 必须走索引！

---

## 第三章：InnoDB 锁的三种物理形态（从零推导）

假设我们的表 `lock_test` 里有主键 `id`，表中现有 3 条记录：`id = 1, 5, 10`。

这 3 条记录在 B+ 树的叶子节点上排成了一排。请在脑海里想象这条物理轴线：

```text
 负无穷 (-∞) ─── [id=1] ─── (间隙 1~5) ─── [id=5] ─── (间隙 5~10) ─── [id=10] ─── 正无穷 (+∞)
```

在这个物理轴线上，InnoDB 提供了 **3 种不同范围的锁**：

---

### 形态 1：记录锁（Record Lock）
*   **定义**：只锁住 B+ 树叶子节点上**某一条确确切切存在的行记录** [2]。
*   **例子**：锁住 `id = 5` 这一行 [2]。
*   **作用**：防止其他事务对 `id = 5` 进行 `UPDATE` 或 `DELETE`。
*   **标记**：在物理日志中记为 `LOCK_REC_NOT_GAP`。

---

### 形态 2：间隙锁（Gap Lock）
*   **定义**：只锁住两条记录之间的**物理空隙（开区间）**，不包含两端的记录本身。
*   **例子**：锁住区间 `(1, 5)`，即大于 1 且小于 5 的范围（不包含 1 和 5）。
*   **为什么需要间隙锁？**
    *   假设事务 A 想读取 `WHERE id BETWEEN 1 AND 5` 的数据。为了防止事务 B 在这期间 `INSERT INTO t (id) VALUES (2)`，事务 A 必须在 `(1, 5)` 这片空地上拉上一圈**铁丝网**。
    *   **作用：防止其他事务在这个间隙里 `INSERT` 新数据，从而彻底防住当前读的“幻读”！**
*   **标记**：在物理日志中记为 `LOCK_GAP`。
*   **独有特性**：间隙锁之间**互不冲突**！事务 A 对 `(1, 5)` 加了间隙锁，事务 B 也可以对 `(1, 5)` 加间隙锁。因为间隙锁的目标只有一种：**阻止第三者 `INSERT` 数据**。

---

### 形态 3：临键锁（Next-Key Lock）
*   **定义**：**记录锁 + 间隙锁 的结合体**，锁住一个 **“左开右闭”的区间 `(左边界, 右边界]`**。
*   **例子**：`(1, 5]`（表示包含了 `(1, 5)` 的间隙，以及 `id = 5` 这行记录本身）。
*   **核心法则（极其重要！）**：
    > 📌 **InnoDB 在 `REPEATABLE READ`（RR，可重复读）隔离级别下，默认的加锁单位就是 Next-Key Lock！**
*   **标记**：在物理日志中记为 `LOCK_ORDINARY`。

---

## 第四章：加锁与“退化/降级”物理法则（一步步逻辑推导）

为什么有时我们执行 SQL，默认的 Next-Key Lock `(1, 5]` 会变成记录锁 `[5]` 或间隙锁 `(1, 5)`？

因为 MySQL 优化器很聪明，它遵循**“能小则小，尽量减少阻塞”**的物理降级法则。

我们依然以表里现有 `id = 1, 5, 10` 为例：

---

### 法则 1：主键/唯一索引等值查询 ➔ 记录存在 ➔ 降级为记录锁（Record Lock）

*   **SQL**：`SELECT * FROM lock_test WHERE id = 5 FOR UPDATE;`
*   **推导过程**：
    1.  默认加锁单位是 Next-Key Lock，锁定区间 **`(1, 5]`**。
    2.  优化器检查：`id` 是 **主键/唯一索引**，且 `id = 5` 这行记录在表里 **真实存在** [2]。
    3.  优化器思考：“既然 `id` 是唯一的，别人绝对不可能再插入一个 `id = 5` 的记录！那我根本没必要锁住 `(1, 5)` 这个间隙，我只需要锁住 `id = 5` 这一行就行了。”
    4.  **降级结果**：Next-Key Lock `(1, 5]` 降级为 **记录锁（Record Lock）`[id = 5]`**。其他事务此时可以自由地在 `(1, 5)` 区间插入 `id = 2, 3, 4` 的数据！

---

### 法则 2：主键/唯一索引等值查询 ➔ 记录不存在 ➔ 降级为间隙锁（Gap Lock）

*   **SQL**：`SELECT * FROM lock_test WHERE id = 3 FOR UPDATE;`
*   **推导过程**：
    1.  优化器在 B+ 树上沿着节点寻找 `id = 3`。
    2.  它越过 `1`，找到了第一个大于 3 的记录 `5`。按照默认规则，给这个区间加上 Next-Key Lock **`(1, 5]`**。
    3.  优化器检查：`id = 3` 这行记录 **不存在**！
    4.  优化器思考：“我这次查询的目标是 3，现在 3 不存在。我只需要防止别人插入 3，我不需要锁住 `id = 5` 这一行真实数据。”
    5.  **降级结果**：锁从右侧的记录 `5` 上退化离开，Next-Key Lock `(1, 5]` 降级为 **间隙锁（Gap Lock）`(1, 5)`**！

---

### 法则 3：普通非唯一索引等值查询 ➔ 锁向右延伸

*   假设 `age` 列是普通二级索引（非唯一），表里有 `age = 1, 5, 10`。
*   **SQL**：`SELECT * FROM lock_test WHERE age = 5 FOR UPDATE;`
*   **推导过程**：
    1.  给 `age = 5` 命中区间加 Next-Key Lock **`(1, 5]`**。
    2.  优化器思考：“因为 `age` 是**非唯一索引**，就算现在表里只有一个 `age = 5`，别人依然有可能插入另一个 `age = 5`！为了防止插入，我必须向右继续扫描，直到找到第一个不等于 5 的记录（10），并对 `(5, 10)` 加间隙锁。”
    3.  **加锁结果**：同时锁定 **`(1, 5]` 的临键锁** + **`(5, 10)` 的间隙锁**！

---

## 第五章：什么是“死锁（Deadlock）”？

### 1. 物理成因
**死锁**是指两个或多个事务在执行过程中，因**互相等待对方持有的锁**而陷入无限期僵持的状态。

#### 物理模拟图：
```text
[ 事务 A ] ──已持有──► [ id = 1 记录锁 ]
    │                      ▲
  尝试获取                已持有
 id = 5 锁              id = 5 锁
    ▼                      │
[ id = 5 记录锁 ] ◄──尝试获取─ [ 事务 B ]
```

1.  事务 A 锁住了 `id = 1`。
2.  事务 B 锁住了 `id = 5`。
3.  事务 A 尝试去 `UPDATE id = 5`（被事务 B 卡住，进入等待）。
4.  事务 B 尝试去 `UPDATE id = 1`（被事务 A 卡住，进入等待）。
5.  两边都在等待对方释放资源，谁也不肯退让，陷入死循环！

---

### 2. MySQL 的死锁检测与自动解套
MySQL 内部有 **死锁检测机制（`innodb_deadlock_detect = ON`）**。

当发现死锁回路时，MySQL 会在毫秒级内自动作出决策：
*   **主动挑选一个“代价较小”的事务（产生 `undo log` 数量较少、修改数据较少的事务）强行回滚（`ROLLBACK`）！**
*   被打掉的那个事务终端会收到报错：`ERROR 1213 (40001): Deadlock found when trying to get lock`。
*   另一个事务则解套顺利执行。

---

## 第六章：Linux 运维辅助命令精讲

在排查高并发死锁或数据库卡死时，我们需要结合以下 Linux 终端命令查看系统状态：

### 1. 磁盘存储排查：`df` 与 `du`
*   **`df -h`（Disk Free）**：
    *   *含义*：查看整个 Linux 系统的磁盘分区剩余空间（`-h` 代表 Human-readable，以 MB/GB 的可读格式显示）。
    *   *用途*：排查是不是磁盘满了导致 MySQL 无法写二进制日志或数据页而卡死。
*   **`du -sh /var/lib/mysql`（Directory Usage）**：
    *   *含义*：专门统计 `/var/lib/mysql` 这个目录总共占用了多少物理磁盘空间（`-s` 代表 Summary 总和，`-h` 代表可读单位）。

### 2. 网络与端口排查：`lsof` 与 `netstat` / `ss`
*   **`lsof -i :3306`（List Open Files）**：
    *   *含义*：列出所有占用 `:3306` 端口的进程及其 PID。
*   **`netstat -tlnp` 或 `ss -tlnp`**：
    *   *含义*：查看系统正在监听（Listening）的所有 TCP 端口。
    *   `t` = TCP 协议，`l` = Listening 监听状态，`n` = 物理数字端口（显示 3306 而非 mysql），`p` = 显示进程 PID。

---

## 💻 第七章：手把手双终端实操演练

请打开两个独立的终端窗口连接 Docker MySQL（**Session A** 和 **Session B**）。

### 步骤 1：准备环境与初始数据

在 **Session A** 运行：
```bash
docker exec -it geo_mysql mysql -uroot -pgeo_secret_123
```

在 MySQL 中粘贴：
```sql
USE geo_platform;

DROP TABLE IF EXISTS `lock_test`;
CREATE TABLE `lock_test` (
  `id` bigint NOT NULL,
  `val` int NOT NULL,
  PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 插入有断层的数据：1, 5, 10
INSERT INTO `lock_test` (id, val) VALUES (1, 100), (5, 500), (10, 1000);
```

在 **Session B** 中也连接 MySQL：
```bash
docker exec -it geo_mysql mysql -uroot -pgeo_secret_123
```

---

### 步骤 2：实操演练 1 —— 间隙锁（Gap Lock）阻塞 `INSERT` 现场

在 RR 隔离级别下，尝试查询一个不存在的 `id = 3`：

#### 顺序交替运行：

1.  **Session A**：开启事务，使用当前读查询不存在的 `id = 3`：
    ```sql
    USE geo_platform;
    BEGIN;
    SELECT * FROM lock_test WHERE id = 3 FOR UPDATE;
    ```
    *物理加锁解析*：因为 `id = 3` 不存在，属于法则 2。Session A 在区间 **`(1, 5)` 上拉起了间隙锁（Gap Lock）**！

2.  **Session B**：开启事务，尝试在该间隙内插入新数据 `id = 2`：
    ```sql
    USE geo_platform;
    BEGIN;
    INSERT INTO lock_test (id, val) VALUES (2, 200);
    ```
    *物理现象*：**Session B 被卡住（阻塞）了！** 命令行光标停住，等待 Session A 释放铁丝网。

3.  **Session A**：提交事务：
    ```sql
    COMMIT;
    ```
    *物理现象*：Session A 提交释放间隙锁的一瞬间，Session B 立刻接触卡顿，插入成功！Session B 随后输入 `COMMIT;`。

---

### 步骤 3：实操演练 2 —— 制造死锁现场与日志排查

#### 顺序交替运行：

1.  **Session A**：锁住 `id = 1`
    ```sql
    BEGIN;
    SELECT * FROM lock_test WHERE id = 1 FOR UPDATE;
    ```
2.  **Session B**：锁住 `id = 5`
    ```sql
    BEGIN;
    SELECT * FROM lock_test WHERE id = 5 FOR UPDATE;
    ```
3.  **Session A**：去申请 `id = 5` 的锁（被 Session B 卡住）
    ```sql
    SELECT * FROM lock_test WHERE id = 5 FOR UPDATE;
    ```
4.  **Session B**：去申请 `id = 1` 的锁（**触发死锁！**）
    ```sql
    SELECT * FROM lock_test WHERE id = 1 FOR UPDATE;
    ```

#### 物理现象：
Session B 回车的一瞬间，终端弹错：
`ERROR 1213 (40001): Deadlock found when trying to get lock; try restarting transaction`
Session B 被 MySQL 强行回滚打飞，Session A 顺利拿到锁解除阻塞！

---

### 步骤 4：查看死锁物理日志

在终端输入：
```sql
SHOW ENGINE INNODB STATUS\G
```
翻到 `LATEST INNODB DEADLOCK` 段落，能清晰看到哪个事务因为等哪条记录的锁导致了回路。

---

## 🏁 Day 5 巩固作业（请回答并在回复中提交）

请根据今天重新推导的物理法则，回答以下 3 道题目：

1. **加锁降级推演题**：
   表 `lock_test` 现存主键 ID 为 `1, 5, 10`（隔离级别为 RR）。
   * 当 Session A 执行 `SELECT * FROM lock_test WHERE id = 5 FOR UPDATE;` 时，根据**降级法则 1**，Session A 最终加的是什么锁？受影响的物理范围是什么？
   * 当 Session A 执行 `SELECT * FROM lock_test WHERE id = 7 FOR UPDATE;` 时，根据**降级法则 2**，Session A 最终加的是什么锁？受影响的物理区间是什么？此时 Session B 如果执行 `INSERT INTO lock_test (id, val) VALUES (8, 800);` 会被阻塞吗？

2. **死锁原理题**：
   * 在什么情况下会产生死锁？当发生死锁时，MySQL 会采用什么策略来解套？
   * 我们可以在 MySQL 终端输入哪条命令来查看最新一次死锁的详细物理日志？

3. **Linux 运维题**：
   * 在 Linux 终端中，`df -h` 与 `du -sh` 这两条命令分别用于排查什么？
   * 如果想查看 3306 端口被哪个 PID 占用，可以使用哪条命令？

请把你的推演步骤和答案回复给我。收到后，我会附上 **Day 5 标准答案**，并带领你进入 **Day 6：Go + GORM 并发一致性实战（悲观锁与乐观锁机制）**！

---

# 📦 第八章：Go 开发视角的锁体系落地（就业方向强化）

> 前面的章节把 MySQL 的锁讲透了。但作为 **Go 后端工程师**，你写的每一行 SQL 都是通过 Go 代码发出的。本章把所有概念映射到 Go 并发原语、GORM 实战和真实业务场景，让你在面试和工作中能**双向打通**。

---

## 8.1 Go 并发原语 vs MySQL 锁 —— 一张对照表记住全部

你在 Go 里其实**早就用过锁**，只是不知道它们和 MySQL 是一回事。对照着看，两边都能记住：

| 概念 | MySQL 中的形态 | Go 中的形态 | 共同本质 |
| :--- | :--- | :--- | :--- |
| **排他锁（X 锁 / 写锁）** | `SELECT ... FOR UPDATE`、`UPDATE`、`DELETE` 加的锁 | `sync.Mutex`（互斥锁） | 同一时刻只允许一个持有者，其他人阻塞等待 |
| **共享锁（S 锁 / 读锁）** | `SELECT ... LOCK IN SHARE MODE` | `sync.RWMutex` 的 `RLock()`（读锁） | 多个读者可同时持有，写者必须独占 |
| **记录锁（Record Lock）** | 锁住单条索引记录 | `sync.Mutex` 保护**单个对象/单行缓存** | 粒度最小，只保护一个目标 |
| **表锁（退化的全表锁）** | 无索引 UPDATE 导致锁全表 | `sync.Mutex` 包住**整个全局 map / 整个缓存** | 粒度最粗，并发度最低 |
| **乐观锁（CAS）** | 版本号 `version` 字段 + `WHERE version = ?` | `sync/atomic` 的 `CompareAndSwap` | 不加锁，靠"比较-交换"检测冲突后重试 |
| **死锁** | 两个事务互相等对方持有的锁 | 两个 goroutine 互相等对方持有的 Mutex | 环形等待，谁也不释放 |

**记忆口诀**：`sync.Mutex` = MySQL 的写锁，`sync.RWMutex` = MySQL 的读写锁，`atomic.CAS` = MySQL 的乐观锁。**同一个并发问题，语言无关，只是表达方式不同。**

---

## 8.2 Go + GORM 悲观锁实战（对应 Day 5 的当前读）

你已经在 Day 5 用 SQL 学过 `SELECT ... FOR UPDATE`。在 Go 里用 GORM 写就是加一个 `clause.Locking`：

```go
// 场景：用户下单扣减余额，必须保证"读到的余额是最新的、且没人同时改"
func DeductBalance(db *gorm.DB, userID uint, amount float64) error {
    return db.Transaction(func(tx *gorm.DB) error {
        // 悲观锁：等价于 SELECT * FROM accounts WHERE id = ? FOR UPDATE
        // 加上锁后，其他事务的 UPDATE/DELETE 会阻塞在这里，直到本事务提交
        var account Account
        if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
            First(&account, userID).Error; err != nil {
            return err
        }

        // 现在可以安全地读-改-写（因为锁保证了没人插队）
        if account.Balance < amount {
            return errors.New("余额不足")
        }
        account.Balance -= amount
        return tx.Save(&account).Error
        // 事务提交时，FOR UPDATE 的锁才会释放
    })
}
```

**对照 Day 5 理解**：
- `clause.Locking{Strength: "UPDATE"}` 就是 `FOR UPDATE`，属于**当前读**。
- 如果 `userID` 走的是**主键/唯一索引**且记录存在，MySQL 会按**降级法则 1** 只加**记录锁**——并发度最高。
- 整个 `db.Transaction` 对应一个事务，**锁的释放时机是事务提交/回滚**，而不是 Go 函数 return。

---

## 8.3 Go + GORM 乐观锁实战（不加锁，靠版本号）

悲观锁适合**冲突频繁**（如扣款），乐观锁适合**冲突少、读多写少**（如点赞、编辑资料）：

```go
type Account struct {
    ID      uint    `gorm:"primaryKey"`
    Balance float64
    Version int     `gorm:"column:version"` // 乐观锁版本号
}

func DeductBalanceOptimistic(db *gorm.DB, userID uint, amount float64) error {
    for attempt := 0; attempt < 3; attempt++ { // 冲突重试机制
        var account Account
        if err := db.First(&account, userID).Error; err != nil {
            return err
        }
        if account.Balance < amount {
            return errors.New("余额不足")
        }

        // 核心：UPDATE ... SET balance=?, version=version+1 WHERE id=? AND version=?
        // 如果期间别人改了这行，version 不匹配，影响行数为 0
        result := db.Model(&Account{}).
            Where("id = ? AND version = ?", userID, account.Version).
            Updates(map[string]any{
                "balance": account.Balance - amount,
                "version": account.Version + 1,
            })

        if result.Error != nil {
            return result.Error
        }
        if result.RowsAffected == 1 {
            return nil // 更新成功
        }
        // RowsAffected == 0 说明版本冲突，别人抢先改了，重试
        fmt.Printf("第 %d 次更新冲突，重试中...\n", attempt+1)
    }
    return errors.New("多次重试仍冲突，请稍后再试")
}
```

**对照 Day 5 理解**：
- 乐观锁**不加任何数据库锁**，用的是 `WHERE version = ?` 这个**条件**来做"比较"，这就是 Go `atomic.CompareAndSwap` 的数据库版。
- 冲突检测靠 `RowsAffected`（影响行数）是否为 1，本质上是 **MySQL 自己判断条件不满足就放弃更新**。
- 优点：读不加锁，并发读性能高；缺点：冲突多时会频繁重试，比悲观锁慢。

---

## 8.4 Go 里的死锁 —— 和 MySQL 死锁是一模一样的病

Go 里两个 goroutine 互相锁对方的 Mutex，也会死锁：

```go
var muA, muB sync.Mutex

func goroutine1() {
    muA.Lock()
    defer muA.Unlock()
    time.Sleep(10 * time.Millisecond)
    muB.Lock() // 等待 goroutine2 释放 muB
    defer muB.Unlock()
}

func goroutine2() {
    muB.Lock()
    defer muB.Unlock()
    time.Sleep(10 * time.Millisecond)
    muA.Lock() // 等待 goroutine1 释放 muA  → 互相等待，死锁！
    defer muA.Unlock()
}
```

**对照 Day 5 的死锁图**：
- 这个图和 Day 5 里事务 A 锁 `id=1` 等 `id=5`、事务 B 锁 `id=5` 等 `id=1` 的环形图**完全等价**。
- Go 运行时检测到死锁会直接 **`fatal error: all goroutines are asleep - deadlock!`** 并 panic；MySQL 则是靠死锁检测器挑一个事务回滚。
- **预防方法也一样**：所有 goroutine/事务**按固定顺序加锁**（比如都先锁 A 再锁 B），就永远不会成环。

---

## 8.5 面试高频题：Go 连接池与锁的"隐藏关系"

面试官常问："为什么 Go 服务偶发超时，查 MySQL 却发现锁等待？"

```go
sqlDB, _ := db.DB()
sqlDB.SetMaxOpenConns(100)   // 最大连接数
sqlDB.SetMaxIdleConns(10)    // 空闲连接数
sqlDB.SetConnMaxLifetime(time.Hour)
```

**关键认知**：
- **长事务 = 长持锁**。如果 Go 代码里 `BEGIN` 后做了慢查询或外部 HTTP 调用（比如调第三方支付），事务一直不提交，**FOR UPDATE 的锁就一直占着**，连接也一直被占用。
- 连接池 `SetMaxOpenConns(100)` 一旦被**长事务占满**，后续所有请求都排队等连接——表现就是"整个服务卡死"，和 MySQL 表锁卡死的现象一样。
- **Go 工程实践**：事务里**绝不放网络 I/O**（HTTP、Redis 长调用），事务范围越小越好，宁可拆成多个短事务。

---

## 8.6 企业级实战：高并发扣库存（面试必考，三套方案对比）

这是 Go 后端面试 90% 会问的场景，正好把 Day 5 的锁知识全部用上：

| 方案 | 实现 | 锁类型 | 优点 | 缺点 |
| :--- | :--- | :--- | :--- | :--- |
| **方案 A：悲观锁** | `SELECT stock FROM goods WHERE id=? FOR UPDATE` 再 UPDATE | 记录锁（走主键，降级法则 1） | 一定不超卖，实现简单 | 高并发下锁等待严重 |
| **方案 B：乐观锁** | `UPDATE goods SET stock=stock-1 WHERE id=? AND stock>0` | 无锁，靠条件判断 | 并发读高 | 高冲突时重试多 |
| **方案 C：Redis 原子减** | `DECR stock`（Redis 单线程天然原子） | Redis 内置原子操作 | 性能最高 | 需处理 Redis 与 MySQL 数据一致性 |

**Go 代码（方案 B 一行核心）**：
```go
// stock > 0 本身就是条件 + 原子 UPDATE，杜绝超卖
result := db.Model(&Goods{}).
    Where("id = ? AND stock > 0", goodsID).
    Update("stock", gorm.Expr("stock - 1"))
if result.RowsAffected == 0 {
    // 没扣成功 → 库存不足或已被抢完
    return errors.New("库存不足")
}
```

**对照 Day 5 理解**：`UPDATE ... WHERE stock > 0` 内部是**当前读 + 排他锁**，`RowsAffected == 0` 时其实"条件不满足、记录没被锁上"，这就是**乐观锁**的思想。想彻底掌握，Day 6 会带你手写 50 协程并发脚本复现超卖并分别用悲观/乐观锁修复。

---

## 🎯 本章小结（Go 工程师必须带走的三句话）

1. **`sync.Mutex` ≈ 写锁，`sync.RWMutex` ≈ 读写锁，`atomic.CAS` ≈ 乐观锁** —— 并发问题的本质在语言间是相通的。
2. **悲观锁用 `clause.Locking{Strength: "UPDATE"}`（`FOR UPDATE`），锁的释放靠事务提交；乐观锁用 `WHERE version = ?` 检测冲突，靠 `RowsAffected` 判断成败。**
3. **锁是资源，持有越久越危险**：MySQL 长事务 = 长持锁，Go 长连接 = 占连接池，两者都会把并发服务拖死。
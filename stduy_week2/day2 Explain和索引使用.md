好的，完全接收你的要求。

我们先给出 **Day 1 三道作业题的工业级标准答案（面试满分文本）**，你可以对照整理到笔记中；随后，我们启动 **Day 2 极高密度精讲**：从优化器 Cost 成本模型、`EXPLAIN` 12 个字段逐字节计算（包括 `key_len` 物理公式），到复合索引 B+ 树内存结构，最后落地到终端 Mock 数据验证。

---

# 📋 Day 1 作业工业级标准答案

### 题目 1：聚簇索引 vs. 二级索引与“回表”物理证明
> **满分标准表达**：
> 1. **物理存储差异**：InnoDB 存储引擎将聚簇索引的叶子节点设计为保存整行记录的所有列字段（包含 `DB_TRX_ID` 事务 ID 和 `DB_ROLL_PTR` 回滚指针） [2]；而二级索引的叶子节点仅包含“索引列的值”与“对应的主键 ID” [2]。
> 2. **回表触发机制**：当执行 `SELECT * FROM ... WHERE scene_id = 'xxx'` 时，优化器首先检索二级索引 B+ 树定位到匹配项，提取出主键 `id`；随后必须拿着该 `id` **重新检索聚簇索引 B+ 树**以获取未保存在二级索引中的其他字段 [2]。这被称为“回表”，会引发额外的磁盘随机 I/O [2]。
> 3. **覆盖索引消灭回表**：当执行 `SELECT id, scene_id ... WHERE scene_id = 'xxx'` 时，所需列完全被二级索引叶子节点覆盖。优化器检测到这一物理事实后，**跳过对聚簇索引的检索**，直接返回二级索引中的数据，触发“覆盖索引（Covering Index）” [2]。`EXPLAIN` 的 `Extra` 列显式标注 `Using index` 正是这一物理优化路径的标志。

### 题目 2：覆盖索引的 EXPLAIN 表现
> **满分标准表达**：`Extra` 字段显式标注 **`Using index`** [2]。

### 题目 3：Linux 权限八进制计算
> **满分标准表达**：
> *   所有者 (Owner) `rwx` = $4 + 2 + 1 = 7$
> *   所属组 (Group) `rw-` = $4 + 2 + 0 = 6$
> *   其他用户 (Others) `---` = $0 + 0 + 0 = 0$
> *   标准命令为：`chmod 760 filename`

---

# 📚 Day 2 深度精讲：MySQL 优化器成本模型、EXPLAIN 全字段剖析与 8 种索引失效

---

## 模块一：MySQL 查询优化器（Optimizer）与 CBO 成本模型

在看 `EXPLAIN` 前，必须明白 MySQL 决定走不走索引的**底层数学法则**：MySQL 使用的是 **基于成本的优化器（Cost-Based Optimizer, CBO）**。

一条 SQL 执行的的总成本（Cost）计算公式为：

$$\text{Cost} = \text{I/O 成本} + \text{CPU 成本}$$

*   **I/O 成本**：从磁盘将数据页加载到内存（Buffer Pool）的成本。MySQL 默认定义读取一个 16KB 页的 I/O 成本值为 `1.0`（MySQL 8.0 调优为 `0.25`）。
*   **CPU 成本**：在内存中检测记录是否满足条件的成本。MySQL 默认定义检测一条记录的 CPU 成本值为 `0.2`（MySQL 8.0 调优为 `0.1`）。

### 为什么有时建立了索引，MySQL 却依然全表扫描？
假设一张表有 10 万行数据，你执行 `SELECT * FROM t WHERE cloud_cover > 10`：
1.  **走二级索引路径的 Cost**：
    *   先扫二级索引树，找到了 8 万条满足 `cloud_cover > 10` 的数据（需要加载 80 个索引页）。
    *   **触发 8 万次回表**！每一次回表都要去聚簇索引树找数据，可能需要读取 8 万个不同的数据页 [2]。
    *   $\text{Cost}_{\text{索引}} = (80 + 80000) \times 1.0 + 80000 \times 0.2 \approx \mathbf{96080}$
2.  **全表扫描路径的 Cost**：
    *   直接加载聚簇索引的所有页（假设整个表占 1000 个数据页）。
    *   $\text{Cost}_{\text{全表}} = 1000 \times 1.0 + 100000 \times 0.2 = \mathbf{21000}$

**数学推导结果**：$\text{Cost}_{\text{全表}} (21000) < \text{Cost}_{\text{索引}} (96080)$。
**结论**：当二级索引筛选出来的数据量较大（通常超过全表数据的 20%~30%）时，优化器认为回表代价太高，会**主动放弃二级索引，选择全表扫描**！

---

## 模块二：EXPLAIN 全部 12 个字段逐字节深度解析

执行 `EXPLAIN SELECT ...` 输出的完整 12 个字段列表如下：

| id | select_type | table | partitions | type | possible_keys | key | key_len | ref | rows | filtered | Extra |
| :---: | :---: | :---: | :---: | :---: | :---: | :---: | :---: | :---: | :---: | :---: | :---: |

### 1. `id`：查询选择器序列号
*   `id` 相同：执行顺序从上到下。
*   `id` 不同：`id` 值越大，优先级越高，越先被执行（如子查询）。

### 2. `select_type`：查询类型
*   `SIMPLE`：简单查询（不包含子查询或 `UNION`）。
*   `PRIMARY`：最外层的查询（如果包含复杂子查询）。
*   `SUBQUERY`：在 `SELECT` 或 `WHERE` 列表中包含的子查询。
*   `DERIVED`：在 `FROM` 列表中包含的子查询（衍生表/临时表）。

### 3. `type`：访问类型（性能阶梯，重点）
从最优到最差排序：

```text
system > const > eq_ref > ref > fulltext > ref_or_null > index_merge > unique_subquery > index_subquery > range > index > ALL
```

*   **`const`**：通过主键或唯一索引做**等值匹配**，最多返回 1 行 [2]。
*   **`eq_ref`**：多表联查（Join）时，驱动表的一条记录，被驱动表只能通过主键或唯一索引匹配到**最多一条记录** [2]。
*   **`ref`**：通过普通二级索引做**等值匹配**，可能匹配到多行 [2]。
*   **`range`**：利用索引做**范围查找**（`>`、`<`、`BETWEEN`、`IN`、`LIKE 'abc%'`） [2]。
*   **`index`**：全索引扫描。把整个二级索引 B+ 树的叶子节点全部扫一遍。通常发生在只查索引列（覆盖索引），但没有 `WHERE` 过滤条件时。
*   **`ALL`**：全表扫描。扫描聚簇索引 B+ 树的全部叶子节点。

---

### 4. `key_len`：索引字节长度计算公式（工业级必备）

`key_len` 表示**优化器实际使用了索引中的多少个字节**。通过 `key_len`，你能精准推算出复合索引 `(a, b, c)` 中到底有几列用上了索引！

#### 物理字节计算法则：

1.  **基础数据类型字节数**：
    *   `TINYINT`: 1 字节 | `SMALLINT`: 2 字节 | `INT`: 4 字节 | `BIGINT`: 8 字节
    *   `FLOAT`: 4 字节 | `DOUBLE`: 8 字节
    *   `DATE`: 3 字节 | `DATETIME`: 5 字节（MySQL 5.6+）| `TIMESTAMP`: 4 字节
2.  **字符集编码系数（针对 `CHAR` / `VARCHAR`）**：
    *   `latin1`: 1 字符 = 1 字节
    *   `utf8` (utf8mb3): 1 字符 = 3 字节
    *   `utf8mb4`: 1 字符 = **4 字节**（MySQL 8.0 默认）
3.  **变长字段长度标识（针对 `VARCHAR`）**：
    *   `VARCHAR` 需要额外的 **2 字节** 存储实际字符串长度。
4.  **NULL 值标识**：
    *   若字段定义允许为 `NULL`（未声明 `NOT NULL`），需额外的 **1 字节** 存储 NULL 标记位。

---

#### 🧮 算力实战演练：

假设表字段定义如下，字符集为 `utf8mb4`：
```sql
`scene_id` VARCHAR(50) DEFAULT NULL  -- 允许 NULL
```
如果一个查询用上了 `scene_id` 索引，它的 `key_len` 物理计算为：

$$\text{key\_len} = (50 \times 4) + 2 (\text{变长长度}) + 1 (\text{NULL 标记}) = \mathbf{203 \text{ 字节}}$$

如果定义为 `scene_id VARCHAR(50) NOT NULL`，则 `key_len` 为 $200 + 2 = \mathbf{202 \text{ 字节}}$。

在复合索引 `idx_a_b_c (a, b, c)` 中，如果 `a` 和 `b` 都是 `INT NOT NULL`（各 4 字节），算出的 `key_len` 是 `8`，代表只有 `a` 和 `b` 用上了索引，`c` 没有用上！

---

### 5. `Extra` 字段：物理执行特征

*   **`Using index`**：触发覆盖索引，无需回表，性能极高 [2]。
*   **`Using index condition`**：触发 **索引下推（ICP）**。MySQL 引擎层在取出二级索引数据时，直接过滤不满足条件的记录，避免不必要的回表。
*   **`Using where`**：存储引擎检索出来数据后，由 MySQL Server 层再进行一次 `WHERE` 过滤。
*   **`Using filesort`**：**危险信号**。需要额外的排序操作（无法利用 B+ 树的天然顺序）。
*   **`Using temporary`**：**致命信号**。使用了隐式内存/磁盘临时表（常见于未走索引的 `GROUP BY` 或 `DISTINCT`）。

---

## 模块三：复合索引物理物理结构与最左匹配原则推导

假设我们建立了复合索引：`KEY idx_a_b_c (a, b, c)`

### 1. 复合索引 B+ 树的底层物理节点结构

在内存/磁盘页中，复合索引的节点是按照 **“字典序（Lexicographical Order）”** 排列的：

```text
                     [ (a=1, b=10, c=100, id=1) ]
                                 /
                                /
  +-------------------------------------------------------------+
  | [a=1, b=10, c=100, id=1]  ->  [a=1, b=20, c=50, id=2]        |
  |                                 |                           |
  |                                 v                           |
  | [a=2, b=5,  c=200, id=3]  ->  [a=2, b=5,  c=300, id=4]        |
  +-------------------------------------------------------------+
```

**物理排序规则**：
1.  **`a` 列全局绝对有序**：物理上先严格按 `a` 升序排列（1, 1, 2, 2）。
2.  **`b` 列局部有序**：只有在 `a` 的值相等的区间内，`b` 才是升序排列的（如 `a=1` 时，`b` 为 10, 20；`a=2` 时，`b` 为 5, 5）。如果脱离 `a` 单独看 `b`（10, 20, 5, 5），它是**彻底无序**的。
3.  **`c` 列局部有序**：只有在 `a` 和 `b` 都相等的区间内，`c` 才是升序排列的（如 `a=2, b=5` 时，`c` 为 200, 300）。

---

### 2. 经典 SQL 在复合索引 B+ 树上的物理寻路推导

#### 场景 1：`WHERE a = 1 AND b = 20 AND c = 50`
*   **寻路过程**：先通过二分法定位到 `a = 1` 的区间 ➔ 在 `a = 1` 区间内二分定位到 `b = 20` ➔ 在 `a = 1 AND b = 20` 区间内二分定位到 `c = 50`。
*   **结果**：三个列全部用上索引，`key_len` 是三列字节总和。

#### 场景 2：`WHERE b = 20 AND c = 50`（缺少最左列 `a`）
*   **寻路过程**：B+ 树根节点是按 `a` 排序的，优化器看整棵树的 `b` 列是完全无序乱码，无法进行任何二分跳跃。
*   **结果**：索引完全失效，退化为 `ALL` 全表扫描。

#### 场景 3：`WHERE a = 1 AND c = 50`（跳过中间列 `b`）
*   **寻路过程**：
    1.  `a = 1` 可以精准二分定位到 `a = 1` 的节点范围。
    2.  但在 `a = 1` 的范围内，`c` 的排列是无序的（因为 `c` 依赖 `b` 的有序性）。
*   **结果**：`a` 用上了索引进行范围定位，但 `c` 无法利用索引做二分查找！`key_len` 只有 `a` 的长度。

#### 场景 4：`WHERE a = 1 AND b > 10 AND c = 50`（中途出现范围查找）
*   **寻路过程**：
    1.  `a = 1` 精准定位。
    2.  `b > 10` 利用双向链表向右做范围扫描（`range`）。
    3.  但在 `b > 10` 的多个不同 `b` 值之间，`c` 的排列是全局无序的！
*   **结果**：`a` 和 `b` 用上了索引，**`b` 后面的字段 `c` 无法使用索引**！

---

## 模块四：8 种经典索引失效场景（物理级深挖）

除了上面讲的最左匹配原则，以下 7 种物理场景同样会导致索引失效：

### 1. 索引列加计算或函数
```sql
-- 错误：WHERE id + 1 = 5;
-- 错误：WHERE DATE_FORMAT(created_at, '%Y') = '2026';
```
*   **物理原因**：B+ 树节点存的是原始值。加了函数/计算后，输出域被改变，优化器无法将计算结果与 B+ 树节点值直接比对。

### 2. 隐式类型转换
```sql
-- scene_id 是 VARCHAR 类型，传入了数字
WHERE scene_id = 12345;
```
*   **物理原因**：MySQL 的转换规则是将 `VARCHAR` 转为 `DOUBLE`，底层隐式套上了 `CAST(scene_id AS UNSIGNED)` 函数，触发场景 1。

### 3. 隐式字符集转换（Collation 冲突）
*   当 `table_A` 的字符集是 `utf8mb3`，`table_B` 的字符集是 `utf8mb4`，两表做 `JOIN` 关联时：
*   **物理原因**：MySQL 会在被驱动表的关联列上套用 `CONVERT()` 函数转换为相同字符集，导致被驱动表的索引彻底失效！

### 4. 头部模糊查询（`LIKE '%abc'`）
*   **物理原因**：字符串 B+ 树从左往右按字符 ASCII 码排序。前缀未知时无法进行二分寻路。

### 5. `OR` 条件未全覆盖索引
```sql
WHERE scene_id = 'A' OR cloud_cover = 10.0 -- cloud_cover 无索引
```
*   **物理原因**：一侧需要全表扫描，整体 Cost 评估高于直接全表扫描，优化器放弃索引。

### 6. `NOT IN`、`!=` 或 `<>` 操作
*   **物理原因**：不等值操作扫描的数据量通常很大，CBO 评估回表 Cost 过高，直接转向全表扫描（注意：`IS NULL` 和 `IS NOT NULL` 如果索引列有 `NOT NULL` 约束或者优化器评估数据少，有时依然能走索引）。

### 7. 选择率（Selectivity）过低 / 数据倾斜
*   如果在 `status` 列（只有 0 和 1）建立索引，表中 99% 的数据 `status = 1`：
*   执行 `WHERE status = 1` 时，由于回表次数高达 99%，优化器算出的 Cost 极大，会**主动放弃索引，选择全表扫描**！

---

## 模块五：Linux 进程管理精讲 (`ps aux`, `top`, `kill`)

在后端开发与排障中，如果线上的 MySQL 或 Go 服务 CPU 飙升到 100%，必须使用 Linux 命令快速定位。

### 1. `ps aux` 输出精讲

运行 `ps aux` 时，输出结果如下：

```text
USER       PID %CPU %MEM    VSZ   RSS TTY      STAT START   TIME COMMAND
mysql      123  5.0 12.3 125800 50200 ?        Ssl  10:00   0:15 mysqld
```

*   **`PID`**：进程 ID（System Call 和 `kill` 命令的靶向目标）。
*   **`VSZ` (Virtual Memory Size)**：虚拟内存大小。包含了进程申请了但尚未实际使用的内存。
*   **`RSS` (Resident Set Size)**：常驻内存大小。**进程实际占用的物理内存（KB）**，排查 OOM 优先看 RSS！
*   **`STAT` (Process State)**：
    *   `R` (Running)：正在运行或在运行队列中。
    *   `S` (Sleeping)：休眠状态（等待事件完成）。
    *   `Z` (Zombie)：**僵尸进程**。子进程已退出，但父进程未回收其资源。
    *   `s`：包含子进程。
    *   `l`：多线程进程。

---

### 2. `top` 交互命令与 Load Average 物理含义

运行 `top` 命令，第一行显示：
`top - 17:30:00 up 10 days, 2:15, 1 user, load average: 0.15, 0.50, 1.20`

*   **`load average`（系统平均负载）**：分别代表 **1分钟、5分钟、15分钟** 内处于 R（运行）和 D（不可中断休眠）状态的平均进程数。
    *   *物理标准*：如果你的 CPU 是 **4 核心**，Load Average 的安全线是 `4.0`。如果 1 分钟 Load 达到了 `12.0`，说明系统等待 CPU 执行的队列严重积压（超载 300%）。

---

### 3. `kill -15` vs. `kill -9` 的物理信号差异

在 Linux 中，`kill` 命令的本质是向目标进程发送一个 **Posix 信号（Signal）**：

*   **`kill -15 PID` (SIGTERM - 终止信号)**：
    *   **优雅退出**。操作系统将信号通知给进程，进程捕获后可以**先关闭数据库连接、将内存缓冲区数据刷入磁盘、释放文件锁**，然后再退出。
    *   **生产环境第一选择**。
*   **`kill -9 PID` (SIGKILL - 强制杀死信号)**：
    *   **内核直接抹杀**。该信号不可被进程捕获或忽略。操作系统内核直接收回进程的内存空间和 PID。
    *   **危害**：如果 MySQL 或 Go 正在写磁盘文件，直接 `kill -9` 会导致文件写入中途断掉，造成物理页损坏或数据丢失！仅在 `kill -15` 无响应时作为终极手段使用。

---

## 💻 模块六：3 小时量级动手实验（VS Code + Terminal）

请打开终端，逐步执行以下命令，验证我们今天讲的所有理论。

---

### 步骤 1：构建复合索引与 Mock 数据

在终端进入 MySQL：
```bash
docker exec -it geo_mysql mysql -uroot -pgeo_secret_123
```

在 MySQL 中执行：
```sql
USE geo_platform;

-- 清空旧表
DROP TABLE IF EXISTS `composite_test`;

-- 创建带有复合索引 idx_a_b_c 的测试表
CREATE TABLE `composite_test` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `a` int NOT NULL,
  `b` int NOT NULL,
  `c` varchar(50) DEFAULT NULL,
  PRIMARY KEY (`id`),
  KEY `idx_a_b_c` (`a`, `b`, `c`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 插入测试数据
INSERT INTO `composite_test` (a, b, c) VALUES 
(1, 10, 'alpha'),
(1, 20, 'beta'),
(2, 5, 'gamma'),
(2, 5, 'delta');
```

---

### 步骤 2：用 `key_len` 验证最左匹配原则（计算演练）

#### 实验 2.1：全命中索引 (a, b, c)
```sql
EXPLAIN SELECT * FROM composite_test WHERE a = 1 AND b = 10 AND c = 'alpha';
```
*   **物理计算 `key_len`**：
    *   `a` (INT NOT NULL) = 4 字节
    *   `b` (INT NOT NULL) = 4 字节
    *   `c` (VARCHAR(50) DEFAULT NULL, utf8mb4) = $50 \times 4 + 2 + 1 = 203$ 字节
    *   **理论总 key_len** = $4 + 4 + 203 = \mathbf{211}$ 字节。
*   **观察终端输出**：查看输出结果中的 `key_len` 是否**精准等于 211**！

#### 实验 2.2：只匹配列 `a`
```sql
EXPLAIN SELECT * FROM composite_test WHERE a = 1 AND c = 'alpha';
```
*   **物理计算 `key_len`**：由于跳过了 `b`，列 `c` 无法使用索引，只有 `a` 命中索引。
*   **观察终端输出**：查看 `key_len` 是否降到了 **`4`**！这精准证明了最左匹配原则的终止。

---

### 步骤 3：验证 `key_len` 表达式与失效

#### 实验 3.1：给索引列施加计算引发失效
```sql
EXPLAIN SELECT * FROM composite_test WHERE a + 1 = 2;
```
*   **观察终端**：查看 `type` 是否变成了 `ALL`，`key` 是否变成了 `NULL`。

---

### 步骤 4：Linux 进程排查实操

退出 MySQL 终端，以 root 进入容器 bash：
```bash
exit;
docker exec -it -u root geo_mysql bash
```

1. **查看 `mysqld` 进程的真实内存（RSS）与 PID**：
   ```bash
   ps aux | grep mysqld
   ```
   *找出输出结果中 `mysqld` 的 PID（假设是 1）和 RSS 的数值（单位是 KB）。*

2. **用 `top` 观察 Load Average**：
   ```bash
   top -b -n 1 | head -n 5
   ```
   *查看第一行的 `load average` 数值。*

---

## 🏁 Day 2 终极作业（请回答并在回复中提交）

请认真完成上述实验，并回答以下 3 道题目：

1. **`key_len` 精准计算题**：
   假设有一张表，字符集为 `utf8mb4`，建有复合索引 `KEY idx_user (age, name)`。
   * `age` 字段定义为：`age INT NOT NULL`
   * `name` 字段定义为：`name VARCHAR(30) DEFAULT NULL`
   * 当我们执行 `SELECT * FROM t WHERE age = 20 AND name = 'Alice';` 时，请写出正确的 `key_len` 计算公式和**最终的八进制/十进制字节数值**。

2. **索引失效与物理分析**：
   在上面的实验 2.2 中，当我们执行 `SELECT * FROM composite_test WHERE a = 1 AND c = 'alpha';` 时：
   * 请问 `Extra` 字段里出现了什么？
   * 请结合 B+ 树的字典序，用你自己的话解释：为什么列 `a` 用到了索引，而列 `c` 无法通过 B+ 树做二分查找？

3. **Linux 信号题**：
   假设在生产环境中，你发现某个后台 Go 数据迁移进程占用了大量的 CPU 资源，你想要停止它。
   * 第一步应该发送什么信号（命令怎么写）？为什么不能直接使用 `kill -9`？
   * `ps aux` 输出中的 `RSS` 字段和 `VSZ` 字段，哪一个代表进程在物理内存中真实占用的空间大小？

请完成这三道题目并在回复中提交。我们明天正式启动 **Day 3：ACID 底层实现（redo/undo log）、4 种隔离级别与双终端并发冲突复现**！
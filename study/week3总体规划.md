# 📌 Week 3：Redis 底层物理原理与高并发缓存实战总体规划（增强版 V2.0）

> **学习周期**：2026.8.8 ～ 2026.8.14（7 天）
> **主线目标**：Redis 八股文脱稿可讲 + Go 分布式锁实战可写 + 项目代码可打标
> **配套资源**：小林 coding（科普主教材）+ 星球八股文 PDF（考点框架）+ go-redis/v9（实战）
> **参考书**：《Redis 设计与实现》——按需查的字典，不是从头读到尾的教材

---

## 🎯 本周验收真值表（周日晚上自查）

完成本周学习后，你必须达到以下标准：

1. **八股文脱稿**：能不看笔记画出 **跳表索引**、**AOF 重写**、**主从增量环形缓冲区** 与 **分布式锁 Lua 解锁** 的物理流程图
2. **代码产出**：`study-notes` 仓库新增 Redis 7 天笔记，且包含一个用 Go 实现的 `go-redis` 分布式锁脚本（含 Watchdog 续期思路）
3. **命令行调优**：能用 Linux 命令行熟练配置远程文件传输（`scp`）及 Git 变基（`rebase`）
4. **项目打标**：在 GeoAgent/GopherAI 项目骨架中，为每个 Redis 使用点标注对应 Day 1-6 的知识点
5. **杀手题**：能对本周 8 道杀手题中的每一道，按「结论 → 分层 → 原因 → 加分」四步脱稿作答

---

## 📚 学习资源使用策略（重要！先读这一段）

**为什么调整**：《Redis 设计与实现》是源码分析书，默认你已熟练使用 Redis 且看得懂 C 代码。直接从头读会劝退。

**本周三层资源法（每学一个主题都按这个顺序走）**：

```
第 1 层 · 动手（最重要）
  在 docker 里跑 redis，用 redis-cli 敲命令、观察结果
  → 把抽象概念变成看得见摸得着的东西

第 2 层 · 科普（建立概念）
  小林 coding 的 Redis 篇（免费，图解+大白话）
  → 弄懂「是什么、为什么这样设计」

第 3 层 · 字典（按需查实现）
  《Redis 设计与实现》对应章节
  → 只在你想深挖「源码到底怎么写的」时才翻，翻完即走
```

**三个铁律**：
1. 先动手，再看书。动手 10 分钟 > 看书 1 小时
2. 《Redis设计与实现》永远不要从头读到尾，按主题查对应章节
3. 面试要的「能画出跳表/AOF/环形缓冲区流程图」——小林 coding 的图 + 你的动手经验就够画出来，不要求你背源码

---

## 🧭 遥感/GIS 映射视角（贯穿本周，帮助建立直觉）

把陌生的 Redis 概念映射到你的专业领域，学一遍顶两遍：

| Redis 概念 | 遥感/GIS 类比 | 记忆锚点 |
|-----------|--------------|---------|
| **SDS** | 带头部元数据的二进制 GeoTIFF 图像流（不依赖 `\0` 截断） | 二进制安全 = 影像二进制流安全 |
| **SkipList（跳表）** | 遥感影像瓦片金字塔（多级索引抽样，$O(\log N)$ 范围检索） | 金字塔层级 = 跳表层级 |
| **AOF / RDB 持久化** | 矢量图层的实时增量编辑日志（AOF）vs 定期全量快照导出（RDB） | 增量编辑 = AOF；全量导出 = RDB |
| **缓存三大天坑** | GIS 高并发瓦片地图服务在缓存未命中时的数据库过载崩塌 | 瓦片缓存击穿 = 热点瓦片过期 |
| **双写一致性** | 空间数据库（PostGIS）与前台瓦片缓存地图的不一致同步问题 | 地图更新了缓存没更新 |
| **分布式锁** | 多节点同时处理同一景影像时的互斥处理 | 防止重复计算同一景 |

---

## ⏰ 每日时间结构（3.5 小时/天）

```
核心八股 + 代码验证    2.25 小时
GeoAgent 预加载/打标    45 分钟
算法 Hot 100（Go 实现） 30 分钟
```

> ⚠️ 如果某天超时：优先保住「核心八股 + 杀手题」，「项目打标」可以顺延到 Day 7 统一做，但**不要砍杀手题**。

---

## 📅 Day 1：Redis 高性能物理本质 + SDS / Hash / List 底层

### 🎯 攻坚目标

1. **为什么 Redis 单线程却这么快？**
   - 纯内存 ns 级随机访问
   - 单线程避免了锁竞争与上下文切换
   - **I/O 多路复用**（epoll 事件驱动 / Reactor 反应堆模型）
   - 高效数据结构（SDS/跳表等——正好衔接 Day 2）
2. **Redis 6.0 多线程物理真相**
   - 多线程**仅用于网络 I/O**（读 socket、解析协议、写回 socket）
   - **命令执行依然是单线程**，避免并发锁问题
   - 配置：`io-threads`（默认关闭） + `io-threads-do-reads`（读需要显式开启）
3. **SDS 底层**
   - `len` / `alloc` / `flags` / `buf` 物理结构
   - 二进制安全、不依赖 `\0`
   - `embstr`（$\le 44$ 字节，一次 `malloc` 内存连续）vs `raw`（$> 44$ 字节，两次 `malloc`）
4. **Hash & List**
   - Hash：`ziplist` / `listpack` → `hashtable`（双 `dictht` **渐进式 Rehash**）的转换阈值与过程
   - List：`ziplist` + `linkedlist` → Redis 3.2+ 的 `quicklist`（双向链表 + 压缩列表）

### 📖 学习路径（三层）

1. **动手**：`docker run -d --name redis-dev -p 6379:6379 redis` 起容器，用 `redis-cli` 依次敲：
   - `SET name "hello"` → `OBJECT ENCODING name`（观察 embstr）
   - `SET big "<46+字符的字符串>"` → `OBJECT ENCODING big`（观察 raw）
   - 反复 `RPUSH` 小列表 → `OBJECT ENCODING`（观察 listpack/quicklist）
   - 反复 `HSET` 小哈希直到超阈值 → `OBJECT ENCODING`（观察何时变 hashtable）
2. **科普**：小林 coding 数据结构篇（SDS / 压缩列表 / 快速列表 / 字典）
3. **字典（可选）**：《Redis 设计与实现》第 1、2、3、8 章（SDS、链表、字典、对象）——只在你想看 `sdshdr` 结构体长什么样时翻

### 🛠️ 动手验证

- `redis-cli` 运行 `OBJECT ENCODING key` 验证类型转换
- 分析 44 字节临界点物理公式（`redisObject` 16 字节 + `sdshdr8` 3 字节 + `\0` 1 字节 ≈ 44 字节内联）

### ⚔️ 杀手题训练

**Q：Redis 单线程怎么处理上万个并发请求的？**

A：基于内核的 epoll 多路复用，将 Socket 事件（可读/可写）压入事件队列，由单线程的事件循环（File Event Handler）无锁顺序排队处理。连接多 ≠ 同时执行命令，命令始终串行执行。

**Q：Redis 6.0 引入了多线程，那 `INCR`、`HSET` 变成多线程并发执行了吗？为什么这样设计？（Day 1 验证题）**

A（按四步）：
1. **结论**：没有。多线程只用于网络 I/O，命令执行依然是单线程。
2. **分层**：6.0 用多线程处理三件事——从 socket 读数据、解析协议、把响应写回 socket。主线程仍独占命令执行、事件循环、dict 渐进式 rehash、过期删除。I/O 线程平时休眠，仅在主线程发现有数据要读/写时才被唤醒并行处理。
3. **原因**：① Redis 高并发瓶颈通常在网络 I/O（上万连接的 syscall、内核/用户态拷贝、上下文切换），这部分可安全并行；② 命令执行保持单线程是核心设计哲学——所有命令天然原子、无需加锁、无数据竞争，一旦命令也多线程就要给全局数据结构加锁，引入死锁复杂度，而命令本身大多 O(1)/O(logN)，收益远小于锁代价。
4. **加分**：Antirez 实测网络 I/O 多线程化后吞吐约提升 2 倍（读场景更明显）；Redis 7.x 及后续命令执行仍是单线程，真正命令级多线程的是分支 KeyDB 和 Valkey 社区版。

### 🛠️ GeoAgent 预加载（45 min）

在本地 GopherAI 原版骨架中查找 `redis.Client` 初始化代码，给 `SET` 操作加日志。

---

## 📅 Day 2：Set / ZSet 跳表 + 大 Key & 热 Key 物理治理

### 🎯 攻坚目标

1. **Set & ZSet 底层**
   - Set：`intset`（有序整数数组 + 二分查找）→ `hashtable`
   - ZSet：`ziplist` / `listpack` → `skiplist`（跳表）+ `hashtable` 联合架构
2. **跳表（SkipList）物理推导**
   - 为什么不用 B+ 树 / 红黑树？（纯内存环境，跳表实现简单、层高概率随机生成、范围查询与 $O(\log N)$ 插入效率极高）
3. **大 Key（BigKey）危害与治理**
   - 危害：主线程阻塞、网络流量倾斜
   - 检测：`redis-cli --bigkeys`
   - 处理：**异步删除 `UNLINK`（代替 `DEL`）**、分片拆分、`HSCAN` 渐进式删除
4. **热 Key（HotKey）治理**
   - 读写分离、本地二级缓存（Go `freecache` / `sync.Map`）、热点 Key 随机后缀分散到不同节点

### 📖 学习路径（三层）

1. **动手**：`SADD` 一组整数 → `OBJECT ENCODING`（观察 intset）；`ZADD` 有序集合 → `ZRANGE / ZSCORE / ZRANGEBYSCORE` 体会有序能力；插入大量数据观察编码变化
2. **科普**：小林 coding 跳表 + 整数集合篇（重点：为什么 ZSet 用跳表不用红黑树/B+树）
3. **字典（可选）**：《Redis 设计与实现》第 5、6 章（跳表、整数集合）——只想看跳表节点结构时翻

### 🛠️ 动手验证

手绘跳表多级索引节点指针结构，推导 $O(\log N)$ 节点查找/插入逻辑。

### ⚔️ 杀手题训练

**Q：生产环境中突然发现一个包含 100 万个元素的 Hash BigKey，直接 `DEL key` 会怎样？怎么正确处理？**

A：`DEL` 是同步阻塞命令，删除大 Key 会直接卡死单线程主循环造成服务假死；正确做法是使用 `UNLINK` 异步释放内存，或用 `HSCAN` 渐进式批次删除。

---

## 📅 Day 3：持久化机制（RDB, AOF）与 Copy-On-Write 物理细节

### 🎯 攻坚目标

1. **RDB 快照**
   - `save`（主进程阻塞）vs `bgsave`（`fork()` 子进程）
   - **Linux Copy-On-Write（COW 写时复制）物理原理**：子进程与父进程共享物理内存页，只有父进程写数据时才复制该内存页
2. **AOF 日志**
   - 写后日志（避免语法检查开销且不阻塞当前写）
   - 3 种刷盘策略：`always` / `everysec` / `no`（安全性 vs 性能权衡）
3. **AOF 重写（`bgrewriteaof`）**
   - 子进程根据当前内存快照直接生成新 AOF
   - 重写期间的写命令存入 **AOF 重写缓冲区**
4. **混合持久化（Redis 4.0+）**
   - AOF 文件开头是 RDB 格式的全量快照，结尾是增量 AOF 日志

### 📖 学习路径（三层）

1. **动手**：在 redis-cli 执行 `CONFIG SET save "60 1"` / `BGSAVE` 生成 RDB；执行 `CONFIG SET appendonly yes` 生成 AOF；对比两种文件内容差异
2. **科普**：小林 coding 持久化篇（RDB vs AOF vs 混合持久化，重点看 COW 图解）
3. **字典（可选）**：《Redis 设计与实现》第 10、11 章（RDB、AOF）——想确认 AOF 重写缓冲区的细节时翻

### 🛠️ 动手验证

修改 `redis.conf` 配置，观察 `.rdb` 和 `.aof` 文件在磁盘上的生成与内容。

### ⚔️ 杀手题训练

**Q：`bgsave` 执行期间，主进程如果修改了 1GB 数据，内存会暴涨 1GB 吗？为什么？**

A：基于 COW 机制，只有被修改的物理内存页（通常 4KB/页）才会被复制，未修改的页依然与子进程共享，因此内存只增加被修改页的大小，不会直接暴涨 1GB。

---

## 📅 Day 4：缓存三大天坑 + 过期删除与 8 种内存淘汰策略

### 🎯 攻坚目标

1. **三大天坑防守**
   - **缓存穿透**（查不存在的数据）：布隆过滤器 / 缓存空值
   - **缓存击穿**（热点 key 过期）：互斥锁重建 / 「永不过期」+ 逻辑过期 + 异步刷新
   - **缓存雪崩**（大量 key 集中过期）：随机 TTL / 多级缓存 / 限流降级
2. **过期 Key 删除策略**
   - **惰性删除**（访问时才查才删，省 CPU 废内存）
   - **定期删除**（随机抽样检查，限制 CPU 执行时长）
3. **内存淘汰策略（8 种，`maxmemory-policy`）**
   - `noeviction`（不淘汰，返回报错）
   - `allkeys-lru` / `volatile-lru`（近似 LRU 采样淘汰）
   - `allkeys-lfu` / `volatile-lfu`（频次计数器）
   - `volatile-ttl` / `allkeys-random` / `volatile-random`

### 📖 学习路径（三层）

1. **动手**：`SET k v EX 5` → 等 5 秒 → `GET k`（观察过期）；`INFO memory` 看内存；`CONFIG SET maxmemory-policy allkeys-lru` 后制造淘汰场景
2. **科普**：小林 coding 缓存问题篇（穿透/击穿/雪崩）+ 过期删除与淘汰策略篇
3. **字典（可选）**：《Redis 设计与实现》第 9 章（数据库与键过期）

### 🛠️ 动手验证

- 在 Go 代码中手写一套「逻辑过期防击穿」的 Go 算法伪代码
- 【可选加分】用 Go 写一个极简布隆过滤器（`bitset + 3 个 hash 函数`，30 行内）

### ⚔️ 杀手题训练

**Q：为什么 Redis 的 key 已经过期了，执行 `INFO memory` 发现内存依然没有下降？**

A：Redis 采用「惰性 + 定期」删除，未被访问且未被定期抽样到的过期 key 依然残留在内存中；只有当后续访问触发惰性删除，或达到淘汰策略触发上限时才会释放内存。

**Q（补充）：「缓存三大天坑」分别是什么？各自怎么解决？**

A：穿透 = 查 DB 不存在的数据（布隆过滤器 / 缓存空值）；击穿 = 热点 key 过期瞬间大量请求打 DB（互斥锁重建 / 逻辑过期异步刷新）；雪崩 = 大量 key 同时过期（随机 TTL / 多级缓存 / 限流降级）。

---

## 📅 Day 5：缓存与 DB 双写一致性 + 分布式锁与 NPC 论战

### 🎯 攻坚目标

1. **缓存一致性（Cache-Aside Pattern）**
   - 读：先读 Cache，未命中读 DB 并写 Cache
   - 写：**先更新 DB，再删除 Cache**
2. **一致性深挖**
   - 为什么是「先更新 DB 再删 Cache」而非「先删 Cache」？（并发写+读时，先删 Cache 会导致读脏数据重建入 Cache）
   - **延时双删**（Delayed Double Delete）
   - **Canal 订阅 MySQL binlog 异步删缓存**
3. **分布式锁物理实现**
   - `SET lock_key unique_val NX PX 30000`
   - 为什么加 `unique_val`：解锁时校验，防止误解锁别人的锁
   - 解锁必须用 **Lua 脚本**：保证「判断 `unique_val` + 删除」两步的原子性
4. **Redlock 与 NPC 争议**
   - 理解 Network Delay（网络延迟）、Process Pause（GC 停顿）、Clock Drift（时钟漂移）对分布式锁的挑战
   - Martin Kleppmann vs Antirez 论战

### 📖 学习路径（三层）

1. **动手**：直接用 go-redis/v9 写分布式锁（见 Day 7 代码骨架）——这是本周唯一以「写 Go 代码」为主的一天
2. **科普**：小林 coding 分布式锁篇 + 缓存一致性篇（先更新 DB 再删 Cache 的图解时序）
3. **字典（可选）**：八股文 PDF 分布式锁与一致性专题；Redlock 争议看 Martin Kleppmann 博客（有余力再看）

### 🛠️ 动手验证

编写 Go 程序，使用 `go-redis` 和 Lua 脚本实现一个线程安全的并发分布式锁。

### ⚔️ 杀手题训练

**Q：「先更新数据库，再删除缓存」在极端情况下会有什么一致性 Bug？怎么解决？**

A：在「读写并发」且「缓存刚好失效」时，读请求查 DB 后的写入缓存动作若晚于写请求的删缓存动作，会导致旧数据落入缓存。解决办法是加分布式锁，或使用「延时双删」/ Canal 消息队列异步重试删除。

---

## 📅 Day 6：高可用架构（主从复制、哨兵 Sentinel、Cluster 集群）

### 🎯 攻坚目标

1. **主从复制**
   - 全量复制（RDB + `replication buffer`）
   - 增量复制（`repl_backlog_buffer` 环形缓冲区，根据 `repl_offset` 断点续传）
2. **哨兵（Sentinel）**
   - Quorum 机制、主观下线（sdown）vs 客观下线（odown）
   - 基于 Raft 理念的领头哨兵选举与自动 Failover
   - 为什么至少 3 个哨兵节点
3. **Cluster 集群**
   - 16384 个哈希槽：`CRC16(key) % 16384`
   - `MOVED`（永久重定向）vs `ASK`（临时重定向）区别

### 📖 学习路径（三层）

1. **动手**：用 docker 起 1 主 1 从（或直接 `replicaof` 配置），敲 `INFO replication` 观察；再起一个哨兵容器看 `SENTINEL get-master-addr-by-name`
2. **科普**：小林 coding 主从/哨兵/集群篇（重点：全量 vs 增量同步的图解、心跳槽位 bitmap）
3. **字典（可选）**：《Redis 设计与实现》第 14、15、16 章（主从、Sentinel、集群）——Cluster 部分相对复杂，第 1 遍可以跳过

### 🛠️ 动手验证

推导 16384 个槽位为什么采用 2KB 位图而不是 16KB 位图的网络传输考量。

### ⚔️ 杀手题训练

**Q：Redis Cluster 的 16384 个槽位信息在节点间心跳包里是怎么传输的？为什么不用 65536？**

A：集群节点间通过 Ping/Pong 心跳包传递 2KB 的 Bitmap（16384 位 = 2048 字节）。如果扩到 65536 位会达到 8KB，心跳包体过大耗费带宽；且 Antirez 认为 Redis 集群节点不建议超过 1000 个，16384 个槽位完全够用。

---

## 📅 Day 7：周度总结 + Linux/Git 实操 + Go-Redis 锁代码实战

### 🎯 攻坚目标

1. **Linux 工具线**（实操）
   - `curl -i -X POST`（带响应头发 POST）
   - `wget` 下载大文件
   - `tar -czvf` / `tar -xzvf` 压缩与解压
   - `ssh-keygen` 配置免密登录
   - `scp` 远程文件传输
2. **Git 工具线**（实操）
   - `git remote add origin`
   - `git fetch` / `git pull --rebase`
   - 本地故意制造一次 merge conflict，用命令行手动编辑 conflict 标记并 `git rebase --continue` 解决
3. **Go 代码终极实战**
   - 用 Go（`go-redis/v9`）手写一个基于 `SET NX PX` + Lua 脚本解锁的可靠分布式锁结构体 `RedisLock`
   - 实现 Watchdog 自动续期（伪代码思路）

### 🛠️ RedisLock 结构体参考骨架

```go
package redislock

import (
    "context"
    "time"

    "github.com/google/uuid"
    "github.com/redis/go-redis/v9"
)

// RedisLock 基于 SET NX PX + Lua 解锁的分布式锁
type RedisLock struct {
    rdb        *redis.Client
    key        string
    value      string // unique_value，用 UUID 保证唯一
    ttl        time.Duration
    watchdogCh chan struct{} // 关闭即停止续期
}

// Lock 尝试加锁（SET lock_key unique_value NX PX ttl）
func (l *RedisLock) Lock(ctx context.Context) (bool, error) {
    ok, err := l.rdb.SetNX(ctx, l.key, l.value, l.ttl).Result()
    if err != nil {
        return false, err
    }
    if ok {
        l.startWatchdog(ctx) // 自动续期，防业务超时后锁被误释放
    }
    return ok, nil
}

// Unlock 用 Lua 脚本原子地「校验 value + 删除」
var unlockScript = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
    return redis.call("DEL", KEYS[1])
else
    return 0
end
`)

func (l *RedisLock) Unlock(ctx context.Context) error {
    return unlockScript.Run(ctx, l.rdb, []string{l.key}, l.value).Err()
}
```

> 面试能说清三件事：① 为什么用 UUID 当 value（防止误删他人锁）；② 为什么解锁用 Lua（判断+删除要原子）；③ Watchdog 为什么存在（业务超时 > TTL 时自动续期，防止锁被提前释放引发并发问题）。

---

## 🔍 本周补充知识点（面试偶尔问，30 分钟内扫完即可）

1. **Redis 事务**：`MULTI` / `EXEC` / `WATCH`
   - 记住三句：`MULTI` 无回滚（命令出错不影响其他命令）；`WATCH` 是乐观锁（key 被改则事务失败）；`pipeline` 是批量发包（减少 RTT，但非原子）
2. **Redis 慢查询**：`slowlog get`，了解 `slowlog-log-slower-than` 配置即可
3. **Redis 内存碎片**：`INFO memory` 里的 `mem_fragmentation_ratio`，过高时用 `activedefrag yes` 或重启

---

## ✅ 周日收尾动作（Day 7 结束后）

1. 对照顶部「验收真值表」逐条自查，未达标的打标，第二天优先补
2. 在 `总计划.md` 的 Redis 部分打勾（□ → ☑）
3. 发布 1 篇周总结笔记到 `study-notes` 仓库，回顾本周踩过的坑
4. **下周预告**：Week 4 = 网络 + 操作系统（TCP/HTTP/epoll/零拷贝）+ Agent Loop 理论预习交叉线

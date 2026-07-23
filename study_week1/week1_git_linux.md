没问题。磨刀不误砍柴工， Linux 基础命令和 Git 命令行是后端开发最基础的“器官”。

我们今天用 **2 轮交互** 彻底拿下这些命令：
*   **第一轮（现在）**：Linux 目录操作 + 文本处理管道（WSL 环境实操）。
*   **第二轮（稍后）**：Git 命令行分支控制与撤销操作（WSL 环境实操）。

---

# 第一轮：Linux 文件导航与文本管道处理

### 1. 先比喻后理论：Linux 管道 `|` 与遥感 Processing Pipeline

在 Linux 中，万物皆文件。
*   **文件路径**：就是 GIS 中的坐标。`pwd` 告诉你当前所在的空间坐标，`cd` 是移动视区，`ls` 是加载当前图层下的所有要素。
*   **管道符 `|`**：就如同遥感影像处理中的 **自动化工作流（Processing Model / Spatial ETL）**。上一步处理的输出（如：波段计算后的结果），**直接作为**下一步处理的输入（如：阈值掩膜），中间不产生任何临时垃圾文件，一气呵成。

---

### 2. 核心命令清单分类速查

#### 📁 A. 目录与文件基础（导航与增删改查）
*   `pwd`：显示当前工作目录的全路径（Print Working Directory）。
*   `cd <路径>`：切换目录（`cd ..` 返回上一级，`cd ~` 返回用户家目录）。
*   `ls -la`：列出当前目录下所有文件（`-a` 看隐藏文件，`-l` 看详细权限与大小）。
*   `mkdir -p a/b`：创建文件夹（`-p` 递归创建多层目录）。
*   `touch <文件名>`：创建一个新的空文件。
*   `cp <源文件> <目标文件>`：复制文件（复制文件夹加 `-r`）。
*   `mv <源文件> <目标位置/新文件名>`：移动文件或重命名文件。
*   `rm -rf <文件或文件夹>`：强制删除（`-r` 递归，`-f` 强制，**极其危险，切记不要写 `rm -rf /`**）。

#### 📄 B. 文件内容查看
*   `cat <文件>`：一次性把整个文件打印到屏幕上（只适合看小文件）。
*   `head -n 5 <文件>`：只看文件的前 5 行。
*   `tail -n 5 <文件>`：只看文件的后 5 行（常用 `tail -f <文件>` 实时追踪动态产生的日志）。
*   `less <文件>`：分页查看大文件（键盘 `PageUp/PageDown` 翻页，`/关键词` 搜索，按 `q` 退出）。

#### 🔍 C. 查找与文本 ETL（组合拳）
*   `find <目录> -name "*.log" -type f`：在指定目录下根据文件名或类型（`-type f` 文件，`-type d` 目录）查找文件。
*   `grep -in "ERROR" <文件>`：在文件内部按关键字行级搜索：
    *   `-i`：忽略大小写（Ignore case）
    *   `-v`：反向匹配，排除包含关键字的行
    *   `-r`：递归搜索子文件夹下所有文件
    *   `-n`：显示匹配行所在的行号
*   `wc -l`：统计输入内容的行数（Word Count - Lines）。
*   `sort`：将文本按字典序排序。
*   `uniq -c`：去重并统计重复次数（**注意：`uniq` 只能去除连续的重复行，所以必须先 `sort` 后 `uniq`**）。

---

### 3. 一题一练：WSL 模拟遥感日志处理 Lab

请打开你的 **WSL 终端**（如 Ubuntu），严格按照以下步骤在终端中一步步敲入命令，完成这个日志分析任务：

#### 🛠️ 步骤一：创建实验工作区
在 WSL 终端中依次敲入命令，建一个项目文件夹：

```bash
cd ~
mkdir -p ~/gis_lab/logs
cd ~/gis_lab/logs
pwd
```
*(确认终端输出了 `/home/你的用户名/gis_lab/logs`)*

#### 🛠️ 步骤二：模拟生成一份卫星处理日志
复制以下命令并回车，这会在当前目录下创建一个名为 `satellite.log` 的测试文件：

```bash
cat << 'EOF' > satellite.log
2026-07-23 10:00:01 INFO  [Landsat8] Tile 120038 download started
2026-07-23 10:00:03 ERROR [Landsat8] Tile 120038 checksum failed
2026-07-23 10:00:05 INFO  [Sentinel2] Tile 51SWR download started
2026-07-23 10:00:08 ERROR [Landsat8] Tile 120038 retry timeout
2026-07-23 10:00:10 WARN  [Sentinel2] Cloud cover high: 85%
2026-07-23 10:00:12 ERROR [YOLOv8] GPU memory out of bounds
2026-07-23 10:00:15 INFO  [Sentinel2] Processing completed
EOF
```

#### 🛠️ 步骤三：管道实操组合拳
现在，请你在 WSL 里依次运行以下三条管道指令，观察输出：

1. **查找所有的 `ERROR` 日志并显示行号**：
   ```bash
   grep -in "ERROR" satellite.log
   ```
2. **统计日志里一共出现了多少条警告（WARN）或错误（ERROR）**（提示：用 `grep` + 管道 `|` + `wc`）：
   ```bash
   grep -E -i "ERROR|WARN" satellite.log | wc -l
   ```
3. **统计发生异常的组件名称分布**（先用 `grep` 找 ERROR，再用 `awk` 提取组件名，最后 `sort | uniq -c`）：
   ```bash
   grep "ERROR" satellite.log | awk '{print $3}' | sort | uniq -c
   ```

---

### 🏁 第一轮作业

请在 WSL 中跑完上述命令，并将**步骤三第 3 条命令（组件名称分布统计）在你的终端里输出的结果**复制发给我。

收到正确输出后，我们立刻进入**第二轮：Git 命令行（分支、Reset 与状态撤销）**的精讲！



结果完全正确。
一个小细节：因为日志里第 3 列是日志级别 `ERROR`，第 4 列才是组件名 `[Landsat8]`。如果要提取组件名，把 `print $3` 换成 `print $4` 即可。这正是 Linux 管道 `|`（数据流过滤）的魅力。

现在进入今天最后一轮攻坚：**第二轮：Git 命令行全流程与撤销回滚**。

---

# 第二轮：Git 命令行全流程与撤销机制

### 1. 先比喻后理论：GIS 版本快照与平行宇宙

告别 GUI，命令行里的 Git 底层模型非常简单：

*   **工作区 (Working Directory)**：你在 QGIS 里正在编辑但**未保存**的矢量要素（磁盘上的实际文件）。
*   **暂存区 (Staging Area/Index)**：你勾选了哪些要素，准备把它们导出存为一个文件包（`git add`）。
*   **版本库 (Repository / Commits)**：一次按了“保存”之后生成的不可变**时空快照**，每次提交都有一个独一无二的 Hash 身份证号（`git commit`）。
*   **分支 (Branch)**：**平行宇宙**。比如主干 `main` 是生产环境图层，你在分支 `feat/yolo` 里随便写 YOLO 算法代码，完全不会污染 `main`。测试没问题后，再把两个宇宙合并（`git merge`）。

#### ⚠️ 核心痛点：`reset --soft` vs `--hard`（面试/实操高频）
当你写错代码，想回退到上一次提交（`HEAD~1`）时：
*   `git reset --soft HEAD~1`：**温和撤销**。把版本号退回上一次，但**保留你写好的代码**（代码退回到暂存区）。适合“我想重新理一下 commit 信息”。
*   `git reset --hard HEAD~1`：**毁灭撤销**。版本号退回上一次，同时**把你工作区改的代码彻底抹去**！物理销毁，不可逆，慎用！

---

### 2. 核心命令清单分类速查

#### 🛠️ A. 基础提交与查看
*   `git init`：在当前目录初始化一个 Git 仓库。
*   `git status`：查看当前代码状态（哪些改动了、哪些在暂存区）。
*   `git add <文件名>`：将文件放入暂存区（`git add .` 暂存所有改动）。
*   `git commit -m "说明信息"`：把暂存区的改动提交为一次正式快照。
*   `git log --oneline`：查看历史提交记录（极简单行模式）。
*   `git diff`：查看工作区代码比暂存区具体改了哪几行。

#### 🌿 B. 分支管理（平行宇宙）
*   `git branch`：查看本地所有分支。
*   `git checkout -b <分支名>` 或 `git switch -c <分支名>`：创建并切入新分支。
*   `git checkout <分支名>` 或 `git switch <分支名>`：切换回已有分支。
*   `git merge <分支名>`：把指定分支的代码合并到当前分支。

#### 🔄 C. 撤销与版本回退
*   `git reset --soft HEAD~1`：撤销最近 1 次提交，保留代码到暂存区。
*   `git reset --hard HEAD~1`：彻底毁灭最近 1 次提交及工作区改动。

---

### 3. 一题一练：WSL 全流程 Git 手操 Lab

请在你的 **WSL 终端**中，严格按照以下步骤，从零建立仓库、建分支合并、并测试版本撤销：

#### 🛠️ 步骤一：创建并初始化 Git 仓库
```bash
cd ~
mkdir -p ~/gis_lab/git_demo
cd ~/gis_lab/git_demo
git init
```

#### 🛠️ 步骤二：在主分支（main/master）完成第 1 次提交
```bash
echo "fmt.Println('v1.0')" > main.go
git add main.go
git commit -m "feat: init main.go v1.0"
git log --oneline
```

#### 🛠️ 步骤三：创建并切入 `feature/yolo` 分支，完成第 2 次提交
```bash
git checkout -b feature/yolo
echo "fmt.Println('yolo model added')" >> main.go
git add main.go
git commit -m "feat: add yolo feature"
git log --oneline
```
*(观察此时应该有 2 条 commit 记录)*

#### 🛠️ 步骤四：切回主分支并合并 `feature/yolo`
```bash
git checkout main   # 如果报错说主分支叫 master，请用 git checkout master
git merge feature/yolo
git log --oneline
```

#### 🛠️ 步骤五：体验 `--soft` 撤销
现在我们把刚才合并上来的提交用 `soft` 方式撤销掉：
```bash
git reset --soft HEAD~1
git status
git log --oneline
```
*(观察 `git status`：代码还在，只是回到了变更被暂存的状态！)*

---

### 🏁 今日最后作业

请跑通上述步骤五后，**复制你在终端运行 `git status` 和 `git log --oneline` 的输出结果**发送给我。

完成后，Linux 与 Git 基础全部补齐，明天我们正式开启 **Day 1：MySQL 物理存储与 `.ibd` 底层攻坚**！.



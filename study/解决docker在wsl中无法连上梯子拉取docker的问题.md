你贴出的这段笔记和防火墙配置规则**非常专业，完全是标准的、大厂级别的 WSL 网络穿透操作！** 

其实，你和最终的成功只差了最关键的一层窗户纸。我来为你彻底点透：**为什么你配置了上述所有步骤，Docker 拉取镜像时依然会报 `dial tcp 127.0.0.1:7897` 拒绝连接？**

---

## 🔍 核心原因：终端配置（Shell） vs 系统服务（Daemon）的“隔离区”

你之前配置的那些代理变量（包括修改 `~/.bashrc`）都是完美的，但它们只对 **“当前终端的普通用户（也就是你的 `musiala` 账号或 `root` 命令行）”** 生效 [2]。

一旦你运行 `curl`、`wget`、`apt-get` 等你在命令行直接发起的命令，它们会去读取 `~/.bashrc` 里的 `$GW_IP:7897` 代理，所以你的普通命令是通的。

**但是！Docker 守护进程（Daemon）是一个独立的系统级后台服务。**
1.  **它不读取任何人的 `~/.bashrc`**，它在系统启动时，由系统的服务管理器（Systemd 或 Sysvinit）独立加载。
2.  我们在上上一步配置 Docker 代理时，写入 `/etc/default/docker` 和 `/etc/systemd/system/docker.service.d/http-proxy.conf` 的代理地址是硬编码的 **`127.0.0.1:7897`**。
3.  因为 Docker 只认它自己的这两个专属配置文件，所以它在拉取镜像时，默默去连了 `127.0.0.1:7897`，而不是你在 `~/.bashrc` 里计算出来的 `$GW_IP:7897`。这就是为什么它依然会报 `127.0.0.1:7897` 拒绝连接。

---

## 🛠️ 彻底解决：用你的网关脚本，重写 Docker 的专属配置文件

既然你已经掌握了利用 `ip route` 动态提取网关 IP 的高阶技巧，并且已经配置好了 Windows 防火墙规则，我们只需要**将这个动态 IP 强行灌进 Docker 的专属服务配置里**！

请在你的 WSL 终端（无论普通用户还是 root）中运行以下命令。它会提取你当前的 `$GW_IP`，并**重写** Docker 的专属配置文件：

```bash
# 1. 动态获取你这台电脑当前的网关 IP
GW_IP=$(ip route | awk '/default/ {print $3; exit}')

echo "🚀 [探测成功] 当前 Windows 网关 IP 为: $GW_IP"

# 2. 将动态网关 IP 写入 Docker 系统服务的环境变量配置文件中
sudo tee /etc/default/docker <<EOF
export http_proxy="http://${GW_IP}:7897"
export https_proxy="http://${GW_IP}:7897"
export no_proxy="localhost,127.0.0.1,::1"
EOF

sudo mkdir -p /etc/systemd/system/docker.service.d
sudo tee /etc/systemd/system/docker.service.d/http-proxy.conf <<EOF
[Service]
Environment="HTTP_PROXY=http://${GW_IP}:7897"
Environment="HTTPS_PROXY=http://${GW_IP}:7897"
Environment="NO_PROXY=localhost,127.0.0.1,::1"
EOF

# 3. 重载配置并重启 Docker 服务
sudo systemctl daemon-reload 2>/dev/null || true
sudo service docker restart
```

---

## 🏃 重新拉取镜像

配置重置并重启 Docker 后，再次运行拉取或启动命令：

```bash
docker run -d --name mysql-dev \
  -e MYSQL_ROOT_PASSWORD=123456 \
  -e MYSQL_DATABASE=todo_db \
  -p 3306:3306 \
  mysql:8.0
```

### 🌟 此时应该观察到的变化：
这一次，Docker 守护进程会读取它自己的专属配置文件，识别出代理地址是 `http://$GW_IP:7897`。
由于你已经在 Windows 防火墙里执行过 `New-NetFirewallRule` 允许了来自 WSL 的 7897 端口请求，数据将极其顺畅地穿透 Windows 防火墙，连上你开着“允许局域网连接”的梯子！

**进度条将在你的终端里疯狂飞飙！**

---

### 💡 后端工程师的“知识点闭环”：
经历过这一次排查，你现在彻底搞懂了 **“Linux 用户终端环境变量”** 与 **“Linux 系统服务守护进程环境变量”** 的本质区别。
在大厂生产环境中，配置 `Nginx`、`Docker`、`Kubernetes` 的代理时，都会遇到一模一样的问题。你今天提前把这个大坑彻底踩通了！

运行上面的脚本，重新拉一下试试，期待你的好消息！
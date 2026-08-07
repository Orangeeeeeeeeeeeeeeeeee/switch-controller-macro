## AI 辅助部署

本项目部署涉及 WSL2 内核编译、蓝牙固件、Python 3.14 patch 等复杂步骤,建议用 AI 助手辅助:
- **deepseek v4 flash + claude code
- **opencode** opencode有免费的deepseek flash 可用

### 提示词

在项目目录 `D:\项目\Switch\` 启动 AI 助手,粘贴以下提示词:

```
我要部署一个 Switch 手柄模拟 + 宏录制 Web UI 项目。项目在当前目录,请先读 README.md 了解完整方案,然后帮我部署:
1. 检查 WSL2 + 自定义内核(kernel/bzImage3)是否就绪(.wslconfig 指向)
2. usbipd 直通蓝牙到 WSL(bind --force + attach --wsl)
3. WSL 里启动 joycontrol web_ui.py(连 Switch,MAC 见项目记忆)
4. 浏览器访问 Web UI(http://<WSL IP>:8080)测试

遇到问题帮我排查:蓝牙 detach、Switch 断开(Connection reset)、Python 3.14 patch、固件加载等。
```

AI 助手能读项目文件 + 记忆,按步骤部署 + 排查问题。

### 扩展功能(本项目暂不支持,AI 可协助开发)

本项目当前支持:**Xbox 无线手柄、键盘、虚拟手柄(Web UI)**。以下**暂不支持**,但可通过 AI 协助扩展:

- **NS 手柄输入**:把真实 Joy-Con / Pro Controller 连 PC,读取其输入转发到 Switch(需 joycontrol 反向 + 手柄 HID 驱动,当前只做"模拟"不做"读取")
- **移植安卓**:把 Web UI / joycontrol 移植到 Android(需 Termux + 蓝牙 HID + aiohttp,架构改动大)
- **其他手柄**:PS5/PS4(Gamepad API 已支持,映射需调)、第三方手柄

如需扩展,告诉 AI 具体需求,它能基于现有架构(`web_ui.py` + WebSocket + joycontrol)开发。

## 维护说明

**本项目无更多精力维护,不接受合并代码(PR)。** 如需修改/扩展,请单开分支(fork)自行维护,谢谢。

## 致谢

本项目使用了以下开源项目:
- [joycontrol](https://github.com/mart1nroo/joycontrol) - Nintendo Switch 蓝牙手柄模拟
- [joycontrol-pluginloader](https://github.com/Almtr/joycontrol-pluginloader) - joycontrol 插件加载器
- [aiohttp](https://github.com/aio-libs/aiohttp) - Python 异步 HTTP/WebSocket
# Switch 手柄模拟 + 宏录制 Web UI

用 PC(WSL2)模拟 Nintendo Switch Pro Controller,通过蓝牙连接 Switch,支持 Xbox 手柄/键盘映射、宏录制与回放。NS 无需破解。


## 支持的硬件

| 硬件 | 说明 |
|---|---|
| **PC 蓝牙适配器** | USB 蓝牙适配器(测试用 MediaTek RZ616 / MT7922,VID 0e8d:0616)。需走 USB 总线(usbipd 可直通)。Intel 蓝牙兼容性差,不推荐。 |
| **Nintendo Switch** | 任意 Switch 主机(未破解),蓝牙可达(约 10 米) |
| **Xbox 手柄**(可选) | Xbox 无线手柄 + Xbox 无线适配器(USB dongle),用于手柄模式实时控制/录制 |
| **键盘**(可选) | 任意键盘,用于键盘模式 |
| **WSL2** | Windows 11 + WSL2 + 自编内核(见下) |

## 原理

```
Xbox 手柄/键盘 ──> 浏览器 Web UI ──WebSocket──> WSL2(joycontrol)──蓝牙──> Switch
                      ↑ 录制/回放/列表/循环
```

- WSL2 里 joycontrol 模拟 Pro Controller,通过 usbipd 直通的蓝牙连 Switch
- 浏览器 Web UI 提供虚拟手柄 + 键盘/手柄映射 + 宏录制/回放
- 回放在 WSL2 后端执行(Python asyncio,微秒级精度)

## 文件

| 文件 | 说明 |
|---|---|
| `web_ui.py` | Web UI 后端(joycontrol 插件 + aiohttp WebSocket server,端口 8080) |
| `web/index.html` | Web UI 前端(虚拟手柄 + 键盘/手柄模式 + 录制/回放/列表/循环) |
| `build_kernel.sh` | WSL2 自定义内核编译脚本(蓝牙+USB+vhci+固件 built-in) |
| `kernel/bzImage3` | 编译好的自定义内核(可直接用)。使用:复制到 `C:\Users\<用户>\wslkernel\`,在 `.wslconfig` 设 `kernel=C:\\Users\\<用户>\\wslkernel\\bzImage3`(或直接指向此项目路径) |
| `joycontrol/` | [joycontrol](https://github.com/mart1nroo/joycontrol) 源码(已 patch Python 3.14 兼容,utils.py) |
| `joycontrol-pluginloader/` | [joycontrol-pluginloader](https://github.com/Almtr/joycontrol-pluginloader) 源码(已 patch,loader.py) |
| `.wslconfig` | WSL 配置(kernel + vmIdleTimeout),复制到 `C:\Users\<用户>\` 使用 |
| `usbipd-win_5.3.0_x64.msi` | usbipd 安装包 |
| `杏仁巢穴宏.json` | 预制宏(用户录制),Web UI 点"读宏"加载 |
| `纠错宏.json` | 预制宏(用户录制),Web UI 点"读宏"加载 |

## 一次性安装(已完成则跳过)

### 1. WSL2 + Ubuntu
```powershell
wsl --install -d Ubuntu
```

### 2. 编译带蓝牙的自定义 WSL 内核
WSL2 默认内核无蓝牙驱动,需自编。见 `build_kernel.sh`:
- 开启 `CONFIG_BT`/`BT_HCIBTUSB`/`BT_HCIBTUSB_MTK`(蓝牙,MediaTek)
- 开启 `CONFIG_USB`/`USB_SUPPORT`(USB)
- `CONFIG_EXTRA_FIRMWARE` 把蓝牙固件 built-in(绕过固件加载器问题)
- 编译后把 `bzImage` 放到 `C:\Users\<用户>\wslkernel\`,在 `C:\Users\<用户>\.wslconfig` 设:
  ```
  [wsl2]
  kernel=C:\\Users\\<用户>\\wslkernel\\bzImage
  vmIdleTimeout=-1
  ```
  (`vmIdleTimeout=-1` 让 WSL 永不空闲停止)

### 3. usbipd(直通蓝牙到 WSL)
下载:https://github.com/dorssel/usbipd-win/releases
```powershell
usbipd bind --busid <蓝牙busid> --force
usbipd attach --wsl --busid <蓝牙busid>
```

### 4. joycontrol + pluginloader(WSL 内)
```bash
sudo apt install python3-dbus libhidapi-hidraw0 python3-pip
git clone https://github.com/mart1nroo/joycontrol.git
git clone --recursive https://github.com/Almtr/joycontrol-pluginloader.git
sudo pip3 install --break-system-packages joycontrol/ joycontrol-pluginloader/
```
**Python 3.14 兼容**:joycontrol 0.15 用 `asyncio.get_event_loop()`(3.14 移除),需 patch `utils.py` 和 `loader.py`(用 `new_event_loop + set_event_loop`)。

### 5. bluez(禁 input 插件)
```bash
sudo apt install bluez
sudo systemctl edit bluetooth
# 加: ExecStart= /usr/libexec/bluetooth/bluetoothd --noplugin=input
sudo systemctl restart bluetooth
```

### 6. 蓝牙固件(若需要)
MediaTek 蓝牙需固件 `BT_RAM_CODE_MT7922_1_1_hdr.bin`。装 `linux-firmware`,若 `.zst` 压缩且内核不支持 zstd,`zstd -d` 解压。或用 `CONFIG_EXTRA_FIRMWARE` built-in(推荐)。

## 日常使用

### 启动
1. 确保 Switch 开机、蓝牙可达
2. usbipd 直通蓝牙(若 detach):
   ```powershell
   usbipd attach --wsl --busid <蓝牙busid>
   ```
3. 启动 Web UI(WSL):
   ```bash
   sudo joycontrol-pluginloader -r <Switch MAC> /mnt/d/项目/Switch/web_ui.py
   ```
   换 Switch(改 MAC,不用改代码):设环境变量 `SWITCH_MAC`:
   ```bash
   SWITCH_MAC=01:23:45:67:89:AB sudo joycontrol-pluginloader -r 01:23:45:67:89:AB /mnt/d/项目/Switch/web_ui.py
   ```
4. 浏览器访问:`http://<WSL IP>:8080`(WSL IP 用 `wsl hostname -I` 查)

### Web UI 功能
- **虚拟手柄模式**:鼠标点页面按键(ABXY/LR/ZLZR/十字键/摇杆)-> Switch
- **键盘模式**:键盘按键按映射表 -> Switch(默认 A/S/Z/X/I/J/K/L/U/O/Q/E 等)
- **手柄模式**:Xbox 手柄(Gamepad API)按映射表 -> Switch
- **映射配置**:页面底部 JSON 框编辑键盘 code / 手柄按钮索引 -> Switch 按键名,点"应用映射"
- **录制**:点"录制"开始,操作(虚拟/键盘/手柄),"停止"结束
- **回放**:点"回放"回放当前宏
- **循环**:`循环` 输入框(1=一次,3=三次,0=无限)
- **间隔**:`间隔`=单宏循环间,`宏间隔`=列表段间,`循环间隔`=列表循环间
- **宏列表**:录制宏 -> "加入列表"(可多个)-> "回放列表"按顺序循环
- **停止**:中断回放/录制
- **存宏/读宏**:宏存 JSON 文件 / 读取
- **预制宏**:两个用户录制的宏,Web UI 点"读宏"加载:
  - `杏仁巢穴宏.json`
  - `纠错宏.json`

## 限制

- **60Hz 量化**:蓝牙/Pro Controller 16ms 报告率(Switch 60fps),按键/摇杆时机量化到 16ms,无法消除(Python 端已微秒精度)
- **无陀螺仪**:Xbox 手柄无陀螺仪,Switch 陀螺仪是 joycontrol 默认 IMU(静态),游戏建议关陀螺仪瞄准
- **蓝牙稳定性**:Switch 超时/睡眠可能断开,重连需重新 `usbipd attach` + 重启 web_ui

## 踩坑记录

详见项目记忆。关键:
- WSL2 内核默认无蓝牙 -> 自编
- 固件加载 -2 -> `CONFIG_EXTRA_FIRMWARE` built-in
- Python 3.14 `get_event_loop` -> patch
- pluginloader 类名必须 = 文件名
- 回放时间戳毫秒/1000 转秒



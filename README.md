# Switch 手柄模拟 + 宏录制 Web UI

用 PC(WSL2)模拟 Nintendo Switch Pro Controller,通过蓝牙连接 Switch,支持 Xbox 手柄/键盘映射、Switch 2 Pro 手柄转发、宏录制与回放。NS 无需破解。

## AI 辅助部署

本项目部署涉及 WSL2 内核编译、蓝牙固件、Python 3.14 patch 等复杂步骤,建议用 AI 助手辅助(如 Claude Code;opencode 有免费 deepseek 可用)。

### 提示词

在项目目录 `D:\项目\Switch\` 启动 AI 助手,粘贴以下提示词:

```
我要部署一个「用 PC 模拟 Nintendo Switch 手柄 + 宏录制 Web UI」项目。项目在当前目录,包含全部源码和编译好的内核。请先读 README.md 了解完整方案,然后:
0. 先识别硬件:查蓝牙适配器(设备管理器「蓝牙」或 usbipd list)确认芯片型号和是否走 USB 总线;查是否有 Xbox 无线手柄。根据蓝牙芯片选内核(复制对应预编译内核为 `kernel/bzImage3`):
   - MediaTek MT7921 -> 复制 `kernel/MT7921 网卡内核` 为 `kernel/bzImage3`
   - MediaTek MT7922/RZ616 -> 复制 `kernel/MT7922 网卡内核` 为 `kernel/bzImage3`
   - Intel AX201 -> 复制 `kernel/Ax201 网卡内核` 为 `kernel/bzImage3`
   - 以上都不匹配 -> 按 build_kernel.sh 自编译(蓝牙固件在 firmware/),产物命名为 `kernel/bzImage3`
   - 蓝牙不走 USB 总线(内置 PCIe/ACPI 等)-> usbipd 直通不可用,需走 USB 总线的蓝牙适配器(MT7921/MT7922/AX201 等 USB 蓝牙均可)
1. 检查/安装 WSL2 + Ubuntu(未装则 wsl --install -d Ubuntu,并开启 systemd)
2. 配置内核:把第 0 步生成/选定的 `kernel/bzImage3` 放 C:\Users\<用户名>\wslkernel\,按 README 配 .wslconfig(kernel + vmIdleTimeout=-1),wsl --shutdown 重启
3. 装 usbipd-win(项目里 msi 或 winget install usbipd.win),直通蓝牙适配器(usbipd list 查 busid,bind --force + attach --wsl)
4. WSL 里装 joycontrol + pluginloader(项目 joycontrol/ 和 joycontrol-pluginloader/ 已 patch Python 3.14,直接 pip3 install 这两个目录)+ aiohttp(Web UI 依赖,joycontrol 不含)+ bluez(禁 input 插件)
5. 配对 Switch:跑 joycontrol-pluginloader/plugins/tests/PairingController.py,Switch 进「更改握法/顺序」菜单,记下 MAC
6. 启动 Web UI:`sudo python3 /mnt/d/项目/Switch/web_ui.py`(Web UI 与连接解耦,自动重连)。Switch MAC 存到 `switch_config.json`(首次启动自动创建、git 已忽略):页面点「连接Switch」自动搜索/连接,或手动编辑该文件
7. (可选,Pro2 模式)WSL 装 `golang libusb-1.0-0-dev python3-dev` + `pip3 install evdev`,`modprobe uinput`;`cd procon2-driver && go build -o procon2-driver ./src`;USB 线连 Switch 2 Pro 手柄,usbipd 直通;`sudo ./procon2-driver --daemon &` 注入 evdev
8. 浏览器访问 http://<WSL IP>:8080 测试(虚拟/键盘/手柄/Pro2 模式 + 绑定/换绑/鼠标摇杆 + 录制/回放/宏列表)

硬件:USB 蓝牙适配器(推荐 MediaTek/Realtek;内置非 USB 蓝牙无法 usbipd 直通)。Xbox 手柄可选。Switch 2 Pro 手柄(Pro2 模式,USB 线)可选。
遇到问题排查:蓝牙 detach、Switch 断开(Connection reset)、固件加载 -2(CONFIG_EXTRA_FIRMWARE built-in)、Switch 12.0+ 连不上(蓝牙设备类 0x002508,/etc/bluetooth/main.conf 兜底)、Python 3.14 get_event_loop(patch utils.py/loader.py)、端口 8080 占用(pkill web_ui.py)、Pro2 无输入(procon2 是否在跑、uinput 是否加载、手柄是否 usbipd 直通)。
```

AI 助手能读项目文件 + 记忆,按步骤部署 + 排查问题。

### 扩展功能(本项目暂不支持,AI 可协助开发)

本项目当前支持:**Xbox 无线手柄、键盘、虚拟手柄(Web UI)、Switch 2 Pro 手柄(Pro2 模式转发)**。以下**暂不支持**,但可通过 AI 协助扩展:

- **旧款 NS 手柄**:Joy-Con / 旧款 Pro Controller 连 PC 转发(当前 Pro2 模式仅支持 Switch 2 Pro;旧款需额外 HID 驱动适配)
- **移植安卓**:把 Web UI / joycontrol 移植到 Android(需 Termux + 蓝牙 HID + aiohttp,架构改动大)
- **其他手柄**:PS5/PS4(Gamepad API 已支持,映射需调)、第三方手柄

如需扩展,告诉 AI 具体需求,它能基于现有架构(`web_ui.py` + WebSocket + joycontrol)开发。

## 支持的硬件

| 硬件 | 说明 |
|---|---|
| **PC 蓝牙适配器** | 蓝牙需走 USB 总线(usbipd 可直通)。测试用 MediaTek RZ616 / MT7922(VID 0e8d:0616);Intel AX201 蓝牙亦走 USB 总线可用(项目附预编译内核)。非 USB 总线(内置 PCIe/ACPI)蓝牙无法 usbipd 直通。 |
| **Nintendo Switch** | 任意 Switch 主机(未破解),蓝牙可达(约 10 米) |
| **Switch 2 Pro 手柄**(可选) | USB 线连 PC,用于 Pro2 模式(输入转发到 Switch) |
| **Xbox 手柄**(可选) | Xbox 无线手柄 + Xbox 无线适配器(USB dongle),用于手柄模式实时控制/录制 |
| **键盘**(可选) | 任意键盘,用于键盘模式 |
| **WSL2** | Windows 11 + WSL2 + 自编内核(见下) |

### 蓝牙适配器兼容性

本项目要求蓝牙**必须走 USB 总线**(usbipd 直通到 WSL)。**可验证的**分类:

| 状态 | 芯片/类型 | 依据 |
|---|---|---|
| ✅ **推荐** | MediaTek MT7921 | 预编译内核 `MT7921 网卡内核` 已内置固件 |
| ✅ **推荐** | MediaTek MT7922 / RZ616 | 本项目实测(RZ616 / MT7922,VID 0e8d:0616),预编译内核 `MT7922 网卡内核` 已内置固件 |
| ✅ **可用** | Intel AX201 | AX201 蓝牙走 USB 总线(usbipd 可直通),预编译内核 `Ax201 网卡内核` 已内置固件 |
| ✅ **推荐** | Realtek USB 蓝牙 | build_kernel.sh 已 enable `BT_HCIBTUSB_REALTEK`,固件在 firmware/ |
| ❌ **不兼容** | 非 USB 总线蓝牙(内置 PCIe/ACPI 蓝牙) | 不走 USB 总线,usbipd 直通不可用(架构硬性限制,与品牌无关) |

> **注**:joycontrol 官方 issue 里没有具体芯片型号的兼容性清单(多为 BlueZ 版本、树莓派、VMware 等软件问题)。除上表(本项目实测 + 架构限制)外,社区偶有关于 Killer(Intel AX200 芯片)、Broadcom、廉价 CSR 适配器的断连报告,但**未经验证,不列入清单**。

判断方法(第 0 步硬件识别):
- 设备管理器「蓝牙」看芯片型号
- `usbipd list` 确认蓝牙是否走 USB 总线(能列出就是 USB)
- 非 USB 总线蓝牙(内置 PCIe/ACPI)-> 换走 USB 总线的蓝牙适配器(MediaTek MT7922 / Intel AX201 / Realtek USB)

## 原理

```
Xbox 手柄/键盘 ──> 浏览器 Web UI ──WebSocket──> WSL2(joycontrol)──蓝牙──> Switch
Switch 2 Pro(USB)──> procon2-driver ──evdev──> Web UI 后端 ─┘
                      ↑ 录制/回放/列表/循环
```

- WSL2 里 joycontrol 模拟 Pro Controller,通过 usbipd 直通的蓝牙连 Switch
- 浏览器 Web UI 提供虚拟手柄 + 键盘/手柄映射 + 宏录制/回放
- Pro2 模式:Switch 2 Pro 手柄经 USB 直通 WSL,procon2-driver 读输入注入 evdev,Web UI 后端转发到 Switch
- 回放在 WSL2 后端执行(Python asyncio,微秒级精度)

## 文件

| 文件 | 说明 |
|---|---|
| `web_ui.py` | Web UI 后端(独立运行,aiohttp WebSocket server,端口 8080;与 Switch 连接解耦) |
| `web/index.html` | Web UI 前端(虚拟/键盘/手柄/Pro2 模式 + 连接/断开 + 录制/回放/列表/循环) |
| `build_kernel.sh` | WSL2 自定义内核编译脚本(蓝牙+USB+vhci+固件+uinput+hidraw built-in) |
| `kernel/MT7921 网卡内核` | 预编译 WSL2 自定义内核(蓝牙+USB+vhci+uinput+hidraw built-in),内置 MediaTek MT7921 蓝牙固件。MT7921 适配器用:复制为 `kernel/bzImage3` 再放到 `C:\Users\<用户>\wslkernel\` |
| `kernel/MT7922 网卡内核` | 预编译 WSL2 自定义内核(蓝牙+USB+vhci+uinput+hidraw built-in),内置 MediaTek MT7922/RZ616 蓝牙固件。MT7922/RZ616 适配器用:复制为 `kernel/bzImage3` 再放到 `C:\Users\<用户>\wslkernel\` |
| `kernel/Ax201 网卡内核` | 预编译 WSL2 自定义内核(蓝牙+USB+vhci+uinput+hidraw built-in),内置 Intel AX201 蓝牙固件。AX201 适配器用:复制为 `kernel/bzImage3` 再放到 `C:\Users\<用户>\wslkernel\` |
| `joycontrol/` | [joycontrol](https://github.com/mart1nro/joycontrol) 源码(已 patch Python 3.14 兼容,utils.py) |
| `joycontrol-pluginloader/` | [joycontrol-pluginloader](https://github.com/Almtr/joycontrol-pluginloader) 源码(已 patch,loader.py) |
| `procon2-driver/` | [procon2-driver](https://github.com/dalmatheo/procon2-driver) 源码(MIT,已 vendor)。读 Switch 2 Pro 手柄 USB 输入并注入为 evdev 虚拟手柄,供 Pro2 模式转发 |
| `.wslconfig` | WSL 配置(kernel + vmIdleTimeout),复制到 `C:\Users\<用户>\` 使用 |
| `usbipd-win_5.3.0_x64.msi` | usbipd 安装包 |
| `firmware/` | MediaTek 蓝牙固件(`BT_RAM_CODE_MT7922_1_1_hdr.bin` 已 built-in 内核,此备份用于重编译) |
| `杏仁巢穴宏.json` | 预制宏(用户录制),Web UI 点"读宏"加载 |
| `纠错宏.json` | 预制宏(用户录制),Web UI 点"读宏"加载 |
| `switch_config.json` | Switch MAC 等隐私配置(**git 已忽略,不上传**),首次启动自动创建 |

## 一次性安装(已完成则跳过)

### 1. WSL2 + Ubuntu
```powershell
wsl --install -d Ubuntu
```

### 2. 配置 WSL 内核
WSL2 默认内核无蓝牙驱动,需自编。项目已附带预编译内核:`kernel/MT7921 网卡内核`、`kernel/MT7922 网卡内核`(MediaTek)、`kernel/Ax201 网卡内核`(Intel AX201)。**把匹配你蓝牙芯片的那个复制为 `kernel/bzImage3`**(再放到 `C:\Users\<用户>\wslkernel\`,见下),即可直接用,无需编译。其他蓝牙芯片才需按 `build_kernel.sh` 自编:
- 开启 `CONFIG_BT`/`BT_HCIBTUSB`/`BT_HCIBTUSB_MTK`(蓝牙,MediaTek)
- 开启 `CONFIG_USB`/`USB_SUPPORT`(USB)
- 开启 `CONFIG_INPUT_UINPUT`/`CONFIG_HIDRAW`(Pro2 模式需要,uinput 默认为模块需 `modprobe uinput`)
- `CONFIG_EXTRA_FIRMWARE` 把蓝牙固件 built-in(绕过固件加载器问题)
- 编译后把 `bzImage` 命名为 `kernel/bzImage3`,放到 `C:\Users\<用户>\wslkernel\`,在 `C:\Users\<用户>\.wslconfig` 设:
  ```
  [wsl2]
  kernel=C:\\Users\\<用户>\\wslkernel\\bzImage3
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
项目已 vendor patched 副本,直接装这两个目录(不要重新 git clone,否则会覆盖成未 patch 版,Python 3.14 报错):
```bash
sudo apt install python3-dbus libhidapi-hidraw0 python3-pip
sudo pip3 install --break-system-packages joycontrol/ joycontrol-pluginloader/ aiohttp
```
`aiohttp` 是 Web UI 后端依赖(joycontrol 不含,需单独装)。**Python 3.14 兼容**:joycontrol 0.15 用 `asyncio.get_event_loop()`(3.14 移除),需 patch `utils.py` 和 `loader.py`(用 `new_event_loop + set_event_loop`)。本项目的 `joycontrol/`、`joycontrol-pluginloader/` 已 patch,可直接 `pip3 install`。

### 5. bluez(禁 input 插件)
```bash
sudo apt install bluez
sudo systemctl edit bluetooth
# 加: ExecStart= /usr/libexec/bluetooth/bluetoothd --noplugin=input
sudo systemctl restart bluetooth
```

### 6. 蓝牙固件(若需要)
MediaTek 蓝牙需固件 `BT_RAM_CODE_MT7922_1_1_hdr.bin`。装 `linux-firmware`,若 `.zst` 压缩且内核不支持 zstd,`zstd -d` 解压。或用 `CONFIG_EXTRA_FIRMWARE` built-in(推荐)。

### 7. Pro2 模式(Switch 2 Pro 手柄转发,可选)
让 Switch 2 Pro 手柄(USB 线连 PC)的输入经 WSL 转发到 Switch。**仅用 Pro2 模式才需要装**,不用可跳过。

1. WSL 内装依赖(编译 procon2-driver 需要 Go + libusb,运行需要 evdev + uinput):
   ```bash
   sudo apt install -y golang libusb-1.0-0-dev python3-dev
   sudo pip3 install evdev
   ```
2. 编译 procon2-driver(项目已 vendor 源码):
   ```bash
   cd /mnt/d/项目/Switch/procon2-driver
   go build -o procon2-driver ./src
   ```
3. 加载 uinput 模块(内核需 `CONFIG_INPUT_UINPUT`,默认为模块 `=m`):
   ```bash
   sudo modprobe uinput
   ls /dev/uinput   # 应存在
   ```
4. USB 线把 Switch 2 Pro 手柄连 PC,usbipd 直通到 WSL:
   ```powershell
   usbipd list                    # 找手柄 busid(VID:PID 057e:2069)
   usbipd bind --busid <busid>
   usbipd attach --wsl --busid <busid>
   ```
5. 后台运行 procon2-driver(读手柄并注入 evdev):
   ```bash
   cd /mnt/d/项目/Switch/procon2-driver
   sudo ./procon2-driver --daemon &
   ```
   看到 `🎮 Player 1` 即成功;`/dev/input/event*` 会出现 "Nintendo Pro Controller 2 (Player 1)" 设备。Web UI 的 NS 手柄读取器会自动按名查找该设备。

## 日常使用

### 启动
1. 确保 Switch 开机、蓝牙可达
2. usbipd 直通蓝牙(若 detach):
   ```powershell
   usbipd attach --wsl --busid <蓝牙busid>
   ```
3. 启动 Web UI(WSL,独立模式,Web UI 与 Switch 连接解耦):
   ```bash
   sudo python3 /mnt/d/项目/Switch/web_ui.py
   ```
   - **Switch MAC 配置**(隐私信息,不进 GitHub):存在项目根目录 `switch_config.json`(已被 .gitignore 忽略,部署时首次启动自动创建)。两种方式填写:
     1. 页面点「**连接Switch**」自动搜索并连接,找到后自动保存到该文件(已配对/已配置时直接连接,不用搜索)
     2. 或手动编辑该文件:
        ```json
        { "switch_mac": "AA:BB:CC:DD:EE:FF" }
        ```
   - 也可临时用环境变量覆盖:`sudo SWITCH_MAC=<MAC> python3 /mnt/d/项目/Switch/web_ui.py`(`sudo` 会丢弃环境变量,必须写在 `sudo` 之后)
   - Web UI 立即启动,与 Switch 连接互相独立:连不上/断连时页面照常可用,后台自动重试(每 2-15 秒)
   - 蓝牙连接卡住时先重置:WSL 里 `hciconfig hci0 reset`(之后 web_ui 会自动重连,不用重启)

### Web UI 功能
- **连接/断开**:「连接Switch」连接(已配置 MAC 直接连;未配置时自动蓝牙搜索,Switch 需进「更改握法/顺序」菜单被发现);「断开」关闭蓝牙连接并停止自动重连,状态栏实时显示连接状态
- **虚拟手柄模式**:鼠标点页面按键(ABXY/LR/ZLZR/十字键/摇杆)-> Switch
- **键盘模式**:键盘按键按映射表 -> Switch(默认 A/S/Z/X/I/J/K/L/U/O/Q/E 等)。WASD 控制左摇杆、IJKL 控制右摇杆(可换绑)
- **手柄模式**:Xbox 手柄(Gamepad API)按映射表 -> Switch
- **Pro2 模式**:Switch 2 Pro 手柄(USB 线直通 WSL)输入直接转发到 Switch,无需映射绑定(仅此模式生效)
- **鼠标**:键盘模式下点"鼠标锁定",鼠标移动控制摇杆(默认右摇杆,可换绑),灵敏度可调
- **绑定**:键盘/手柄模式下点 Switch 按键(高亮),再按键盘键/手柄按钮即可绑定,按钮上显示绑定键
- **摇杆换绑**:点摇杆下方"换绑" -> 键盘模式选 keyboard/mouse;手柄模式转一圈自动检测
- **宏列表**:录制 -> "加入列表"(可多个)-> "回放列表"按顺序循环;宏间隔(段间)/循环间隔(循环间)
- **映射配置**:页面底部 JSON 框编辑键盘 code / 手柄按钮索引 -> Switch 按键名,点"应用映射"
- **录制**:点"录制"开始,操作(虚拟/键盘/手柄),"停止"结束
- **回放**:点"回放"回放当前宏
- **循环**:`循环` 输入框(1=一次,3=三次,0=无限)
- **间隔**:`间隔`=单宏循环间,`宏间隔`=列表段间,`循环间隔`=列表循环间
- **停止**:中断回放/录制(列表宏的长间隔也能即时中断)
- **存宏/读宏**:宏存 JSON 文件 / 读取
- **预制宏**:两个用户录制的宏,Web UI 点"读宏"加载:
  - `杏仁巢穴宏.json`
  - `纠错宏.json`

## 限制

- **60Hz 量化**:蓝牙/Pro Controller 16ms 报告率(Switch 60fps),按键/摇杆时机量化到 16ms,无法消除(Python 端已微秒精度)
- **无陀螺仪**:Xbox 手柄无陀螺仪,Switch 陀螺仪是 joycontrol 默认 IMU(静态),游戏建议关陀螺仪瞄准
- **蓝牙稳定性**:Switch 超时/睡眠可能断开;web_ui 后台自动重连,不用手动重启。若蓝牙适配器 detach(usbipd 断开)才需重新 `usbipd attach`

## 踩坑记录

关键:
- WSL2 内核默认无蓝牙 -> 自编
- 固件加载 -2 -> `CONFIG_EXTRA_FIRMWARE` built-in
- Python 3.14 `get_event_loop` -> patch utils.py / loader.py
- pluginloader 类名必须 = 文件名
- 回放时间戳毫秒/1000 转秒
- L2CAP 阻塞 connect 冻结事件循环 -> 线程池 + settimeout
- Switch 断开后持连接槽约 8 秒(重连报 Connection refused)-> 断开/重连已自动重置蓝牙(hci0 reset)释放
- 蓝牙设备类须为 `0x002508`(Switch 12.0+ 要求)-> 项目 joycontrol 已在 SDP 注册后 set_class;若仍连不上,`/etc/bluetooth/main.conf` 设 `Class = 0x002508` 兜底(见下文,[joycontrol#20](https://github.com/mart1nro/joycontrol/issues/20))

### 蓝牙设备类 0x002508(Switch 12.0+ 连不上)
Switch 12.0+ 要求蓝牙设备类为 `0x002508`(Gamepad/joystick),否则连不上或即断。joycontrol 会自动设置,但注册 SDP 记录后可能被重置([joycontrol issue #20](https://github.com/mart1nro/joycontrol/issues/20))。本项目 joycontrol 已按 #20 修法把 `set_class()` 放到 `register_sdp_record()` 之后(`joycontrol/joycontrol/server.py`),一般无需手动处理。个别适配器上仍不生效时,持久化兜底:
```bash
# /etc/bluetooth/main.conf 加一行
Class = 0x002508
```
改完 `sudo systemctl restart bluetooth`。或临时:`sudo hciconfig hci0 class 0x002508`(每次运行 joycontrol 前执行,joycontrol 启动会重置)。

## 更新日志

### 2026-08-13
- **新增 MT7921 预编译内核**:`kernel/MT7921 网卡内核`(MediaTek MT7921 蓝牙固件 built-in),与 MT7922/AX201 一样按芯片复制为 `kernel/bzImage3` 直接使用
- **Xbox Series 手柄适配**:「检测手柄」按钮(仅手柄模式显示);全模式自动识别手柄(`getXboxGamepad` 优先 Xbox/XInput,不再死取第一个);「手柄映射」面板常驻检测显示;默认映射补 Xbox 键(16)→Home、Share 键(17)→截图
- **录制/播放 60Hz 对齐**:摇杆录制与播放都按 16ms 节流,和 Switch 60Hz 采样对齐,快速摇杆动作不再因采样丢位置被吞
- **宏回放连接卡死修复**:`controller_state.connect()` 加 15s 超时(Switch 握手中途断开时不再永久卡死 conn_manager,自动重试)
- **停止复位**:点「停止」释放全部按键 + 摇杆回中,宏停到一半不再卡住按键
- **录制禁回放**:录制中点回放被禁止(前端 toast 提示 + 后端忽略);改用非模态 toast,避免弹窗确定键的点击穿透被录进宏
- **宏重放竞态修复**:代计数器 `_play_gen`,重放/停止时旧任务即退、不误停新任务、停止不丢(上一提交遗漏,补记)

### 2026-08-12
- **文档:蓝牙设备类 0x002508 兜底**:Switch 12.0+ 要求蓝牙类 0x002508,项目 joycontrol 已在 SDP 注册后 set_class;个别适配器不生效时 `/etc/bluetooth/main.conf` 设 `Class = 0x002508` 兜底([joycontrol#20](https://github.com/mart1nro/joycontrol/issues/20))
- **新增 Intel AX201 预编译内核**:`kernel/Ax201 网卡内核`(AX201 蓝牙走 USB 总线,可 usbipd 直通,固件已内置);MT7922 预编译内核重命名为 `kernel/MT7922 网卡内核`。部署按蓝牙芯片复制对应内核为 `kernel/bzImage3`(MT7922/AX201 均有预编译,其他芯片自编译)
- **额外宏列表**:主列表循环 N 次或 T 时间后,在该轮结束插入运行一次额外宏列表(周期重复);前端实时提示未设触发条件
- **宏重放竞态修复**:代计数器 `_play_gen`,重放/停止时旧任务即退、不误停新任务、停止不丢

### 2026-08-11
- **Pro2 模式支持录制宏**:Pro2 输入(后端)广播回前端录制;按钮全录,摇杆只录变化(>0.05 阈值,防高频事件撑爆宏)
- **断开/重连自动释放 Switch 连接槽**:断开、重连、连接按钮遇 Connection refused 时自动重置蓝牙(`hciconfig hci0 reset`)强制释放,不再需要等 8 秒或手动重置
- **procon2-driver 全面修复**(vendored 源码,编译通过):
  - `runReadLoop` 瞬时 USB 错误不再死亡(原:断连循环)
  - goroutine 加 recover,panic 不再杀死整个驱动
  - `SendInitSequence` 失败返回错误(原:吞错误导致"连上即断")
  - uinput 写 EAGAIN 重试 + 全部 ioctl 错误检查
  - `parseReport` 校验 report ID(消除幽灵按键)
  - 移除 SetAutoDetach(会 detach usbhid 导致 hidraw 消失)
  - 扫描 goroutine 停止机制、设备句柄泄漏修复、`os` 包替换废弃 `ioutil`
- **蓝牙断连后自动恢复**:修复 joycontrol `connection_lost` 对 Task 调 `set_exception` 的 RuntimeError(原:断连后卡死不重连)

### 2026-08-10
- **Xbox 手柄后台断连修复**:gpLoop 从 `requestAnimationFrame` 改 `setInterval`(浏览器后台标签页不再暂停手柄轮询)
- **宏间隔/列表间隔吞按键修复**:去掉 busy-wait(阻塞事件循环)、间隔改用真实时间、每个宏/循环开始时释放所有按键
- **Web UI 按键无法按下修复**:按钮 `onmousedown` 的 `\'` 是 JS 语法错误,改普通引号
- README 链接修复(joycontrol `mart1nroo` → `mart1nro`)

### 2026-08-08
- **Web UI 与 Switch 连接解耦**:web 服务器先启动,连接放后台重试,断连/连不上页面照常可用
- **连接/断开/重连按钮**:「连接Switch」(已配置直连,未配置自动蓝牙搜索)、「断开」(清理状态)、「重连」
- **Switch MAC 配置**:`switch_config.json`(git 忽略、部署自动创建),页面设置或环境变量 `SWITCH_MAC`
- **Pro2 模式**:Switch 2 Pro 手柄(USB 直通 WSL)输入转发到 Switch,无需映射;vendor procon2-driver 源码
- **状态实时推送**:连接状态(连接中/已连接/已断开)实时显示
- 修复:A/B 按钮映射(procon2 用 Xbox 命名)、宏列表停止(长间隔可中断)、蓝牙连接挂死(sock_connect 线程池 + 超时)

## 维护说明

**本项目无更多精力维护,不接受合并代码(PR)。** 如需修改/扩展,请单开分支(fork)自行维护,谢谢。

## 致谢

本项目使用了以下开源项目:
- [joycontrol](https://github.com/mart1nro/joycontrol) - Nintendo Switch 蓝牙手柄模拟
- [joycontrol-pluginloader](https://github.com/Almtr/joycontrol-pluginloader) - joycontrol 插件加载器
- [procon2-driver](https://github.com/dalmatheo/procon2-driver) - Switch 2 Pro Controller Linux 驱动(MIT)
- [aiohttp](https://github.com/aio-libs/aiohttp) - Python 异步 HTTP/WebSocket

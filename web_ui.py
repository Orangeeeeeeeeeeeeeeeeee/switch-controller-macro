import asyncio
import json
import logging
import os
import re
import sys
import threading
import time
from aiohttp import web, WSMsgType

logger = logging.getLogger('web_ui')

PROJECT_DIR = os.path.dirname(os.path.abspath(__file__))
WEB_DIR = os.path.join(PROJECT_DIR, 'web')
CONFIG_PATH = os.path.join(PROJECT_DIR, 'switch_config.json')


class web_ui:
    def __init__(self, controller_state=None):
        # controller_state is passed when loaded via joycontrol-pluginloader;
        # when running standalone (python3 web_ui.py) it starts as None and the
        # background _conn_manager establishes it.
        self.controller_state = controller_state
        self._playing = False
        self._pro2_enabled = False
        self._reconnect_lock = asyncio.Lock()
        self._loop = None
        self._switch_mac = ''
        self._clients = set()
        self._transport = None
        self._auto_reconnect = True
        self._load_config()

    def _load_config(self):
        # Private info (Switch MAC, ...) lives in switch_config.json which is
        # gitignored and auto-created on deploy - never commit it.
        try:
            with open(CONFIG_PATH, encoding='utf-8') as f:
                cfg = json.load(f)
            self._switch_mac = (cfg.get('switch_mac') or '').strip().upper()
        except FileNotFoundError:
            self._switch_mac = ''
        except Exception as e:
            logger.warning(f'config load failed: {e}')
            self._switch_mac = ''

    def _save_config(self):
        with open(CONFIG_PATH, 'w', encoding='utf-8') as f:
            json.dump({'switch_mac': self._switch_mac}, f, ensure_ascii=False, indent=2)

    def _get_mac(self):
        mac = os.environ.get('SWITCH_MAC', '').strip().upper()
        if not mac:
            mac = self._switch_mac
        if not mac:
            args = sys.argv[1:]
            for i, a in enumerate(args):
                if a in ('-r', '--reconnect_bt_addr') and i + 1 < len(args):
                    mac = args[i + 1].strip().upper()
        return mac

    async def run(self):
        self._loop = asyncio.get_running_loop()
        self._playing = False
        self._reconnect_lock = asyncio.Lock()
        self._pro2_enabled = False
        if not os.path.exists(CONFIG_PATH):
            try:
                self._save_config()
                logger.info(f'config auto-created: {CONFIG_PATH}')
            except Exception as e:
                logger.warning(f'cannot create config: {e}')
        threading.Thread(target=self._ns_reader_thread, daemon=True).start()

        # Web server starts FIRST, independent of the Switch connection, so the
        # UI is always reachable even while connecting / retrying in the background.
        app = web.Application()
        app.router.add_get('/', self._index)
        app.router.add_get('/ws', self._ws_handler)
        if os.path.isdir(WEB_DIR):
            app.router.add_static('/', WEB_DIR)
        runner = web.AppRunner(app)
        await runner.setup()
        site = web.TCPSite(runner, '0.0.0.0', 8080)
        await site.start()
        logger.info('Web UI on http://0.0.0.0:8080')

        asyncio.create_task(self._conn_manager())
        await asyncio.Event().wait()

    async def _conn_manager(self):
        # Owns the Switch connection + 60Hz keepalive. Retries forever when the
        # Switch is unreachable; reconnects on drops. Connect is non-blocking
        # (patched server.py uses sock_connect + timeout), so the web server
        # never freezes.
        fail = 0
        delay = 2
        while True:
            if self.controller_state is None:
                if not self._auto_reconnect:
                    # user disconnected manually - stay disconnected until asked
                    await asyncio.sleep(1)
                    continue
                try:
                    await self._reconnect()
                    fail = 0
                    delay = 2
                    logger.info('connected to Switch')
                    self._broadcast_status('connected', '已连接 Switch')
                except asyncio.CancelledError:
                    raise
                except Exception as e:
                    fail += 1
                    # "Connection refused" means the Switch still holds the
                    # controller slot from a recent drop; fast retries reset its
                    # release timer so it never frees - wait a quiet period.
                    # Other errors (host down, timeout) use normal backoff.
                    es = str(e).lower()
                    quiet = 8 if ('refused' in es or 'errno 111' in es) else delay
                    if fail <= 3 or fail % 5 == 0:
                        logger.warning(f'connect failed: {e}, retry in {quiet}s')
                    await asyncio.sleep(quiet)
                    delay = min(delay + 2, 15)
                continue
            try:
                await self.controller_state.send()
                fail = 0
            except asyncio.CancelledError:
                raise
            except Exception:
                fail += 1
                if fail >= 10:
                    logger.info('connection lost, reconnecting...')
                    self._broadcast_status('disconnected', '已断开,重连中...')
                    self.controller_state = None
                    self._broadcast_status('connecting', '连接中...')
                    fail = 0
            await asyncio.sleep(1 / 60)

    async def _index(self, request):
        return web.FileResponse(os.path.join(WEB_DIR, 'index.html'))

    async def _ws_handler(self, request):
        ws = web.WebSocketResponse()
        await ws.prepare(request)
        self._clients.add(ws)
        async for msg in ws:
            if msg.type == WSMsgType.TEXT:
                try:
                    ev = json.loads(msg.data)
                    if ev.get('type') == 'play':
                        self._playing = False
                        await asyncio.sleep(0)
                        asyncio.create_task(self._play(ev.get('macro', []), ev.get('loops', 1), ev.get('interval', 0)))
                    elif ev.get('type') == 'play_list':
                        self._playing = False
                        await asyncio.sleep(0)
                        asyncio.create_task(self._play_list(ev.get('macros', []), ev.get('loops', 1), ev.get('macroInterval', 0), ev.get('loopInterval', 0)))
                    elif ev.get('type') == 'stop':
                        self._playing = False
                        logger.info('stop received')
                    elif ev.get('type') == 'reconnect':
                        asyncio.create_task(self._force_reconnect())
                    elif ev.get('type') == 'connect':
                        mac = (ev.get('mac') or '').strip().upper()
                        if mac:
                            self._switch_mac = mac
                            try:
                                self._save_config()
                                logger.info('Switch MAC saved to config')
                            except Exception as e:
                                logger.warning(f'save config failed: {e}')
                        asyncio.create_task(self._connect_from_ui())
                    elif ev.get('type') == 'disconnect':
                        asyncio.create_task(self._disconnect())
                    elif ev.get('type') == 'pro2':
                        self._pro2_enabled = bool(ev.get('enabled'))
                        logger.info(f'pro2 mode enabled={self._pro2_enabled}')
                    elif ev.get('type') == 'get_status':
                        if self.controller_state is not None:
                            state, msg = 'connected', '已连接 Switch'
                        elif not self._auto_reconnect:
                            state, msg = 'disconnected', '已断开'
                        else:
                            state, msg = 'connecting', '连接中...'
                        try:
                            await ws.send_json({'type': 'status', 'state': state, 'msg': msg})
                        except Exception:
                            pass
                    else:
                        await self._apply(ev)
                except Exception as e:
                    logger.warning(f'ws apply error: {e}')
            elif msg.type == WSMsgType.ERROR:
                logger.warning(f'ws error: {ws.exception()}')
        self._clients.discard(ws)
        return ws

    def _broadcast_status(self, state, msg):
        async def _send(ws):
            try:
                await ws.send_json({'type': 'status', 'state': state, 'msg': msg})
            except Exception:
                pass
        for ws in list(self._clients):
            try:
                asyncio.create_task(_send(ws))
            except Exception:
                pass

    async def _interruptible_sleep(self, seconds):
        # Sleep in small chunks so 'stop' interrupts long macro gaps promptly.
        # A single long asyncio.sleep() is not interruptible - stop would wait
        # for the whole gap (macros can have 10s+ pauses) before taking effect.
        end = time.time() + seconds
        while self._playing and time.time() < end:
            await asyncio.sleep(min(0.05, end - time.time()))

    async def _play(self, macro, loops=1, interval=0):
        if not macro:
            logger.info('play: empty macro')
            return
        logger.info(f'playing {len(macro)} events, loops={loops}, interval={interval}s')
        self._playing = True
        loop_count = 0
        while self._playing and (loops == 0 or loop_count < loops):
            loop_count += 1
            t0 = macro[0]['t'] / 1000.0
            start = time.time()
            for i, e in enumerate(macro):
                if not self._playing:
                    break
                target = e['t'] / 1000.0 - t0
                now = time.time() - start
                if target > now:
                    remaining = target - now
                    if remaining > 0.003:
                        await self._interruptible_sleep(remaining - 0.003)
                    if not self._playing:
                        break
                    while time.time() - start < target:
                        pass
                if not self._playing:
                    break
                await self._apply(e['ev'])
            if self._playing and interval > 0 and (loops == 0 or loop_count < loops):
                slept = 0
                while self._playing and slept < interval:
                    await asyncio.sleep(0.1)
                    slept += 0.1
        self._playing = False
        logger.info('play done')

    def _ns_reader_thread(self):
        # Runs in a dedicated thread: evdev read_loop() blocks, so it must NOT
        # run on the asyncio event loop (it would freeze the web server).
        # Retries: if the controller disappears (USB replug / procon2 restart)
        # read_loop() raises - reopen instead of dying silently.
        import evdev
        BTN_MAP = {
            304: 'a', 305: 'b', 307: 'x', 308: 'y',
            310: 'l', 311: 'r', 312: 'zl', 313: 'zr',
            314: 'minus', 315: 'plus', 316: 'home', 317: 'l_stick', 318: 'r_stick',
            544: 'up', 545: 'down', 546: 'left', 547: 'right'
        }
        STICK_AXIS = {0: ('left', 'h'), 1: ('left', 'v'), 3: ('right', 'h'), 4: ('right', 'v')}
        stick = {'left': {'h': 0.0, 'v': 0.0}, 'right': {'h': 0.0, 'v': 0.0}}
        logger.info('NS controller reader started')
        while True:
            dev = self._open_ns_device(evdev)
            if dev is None:
                time.sleep(3)
                continue
            logger.info(f'ns controller: {dev.name} ({dev.path})')
            try:
                for e in dev.read_loop():
                    try:
                        if not self._pro2_enabled:
                            continue
                        if e.type == 1:
                            name = BTN_MAP.get(e.code)
                            if name:
                                self._apply_ts({'type': 'button', 'name': name, 'pressed': bool(e.value)})
                        elif e.type == 3:
                            if e.code in STICK_AXIS:
                                side, axis = STICK_AXIS[e.code]
                                nv = e.value / 32767.0
                                if abs(nv) < 0.1:
                                    nv = 0.0
                                if axis == 'v':
                                    nv = -nv
                                stick[side][axis] = nv
                                self._apply_ts({'type': 'stick', 'stick': side, 'h': stick[side]['h'], 'v': stick[side]['v']})
                    except Exception as ex:
                        logger.warning(f'ns apply error: {ex}')
            except Exception as e:
                logger.warning(f'ns reader loop error: {e}, reopening in 3s')
            try:
                dev.close()
            except Exception:
                pass
            stick = {'left': {'h': 0.0, 'v': 0.0}, 'right': {'h': 0.0, 'v': 0.0}}
            time.sleep(3)

    def _open_ns_device(self, evdev):
        # Find the procon2-injected virtual gamepad by name; fall back to event1.
        try:
            for path in evdev.list_devices():
                try:
                    d = evdev.InputDevice(path)
                    nl = d.name.lower()
                    if 'pro controller 2' in nl or 'procon2' in nl:
                        return d
                    d.close()
                except Exception:
                    pass
        except Exception:
            pass
        try:
            return evdev.InputDevice('/dev/input/event1')
        except Exception as e:
            logger.warning(f'ns controller not found: {e}')
            return None

    def _apply_ts(self, ev):
        try:
            asyncio.run_coroutine_threadsafe(self._apply(ev), self._loop)
        except Exception as e:
            logger.warning(f'ns dispatch error: {e}')

    async def _connect_from_ui(self):
        # Triggered by the "连接 Switch" button. If already connected, just
        # report it - a second L2CAP connect would be refused by the Switch.
        if self.controller_state is not None:
            logger.info('already connected to Switch')
            self._broadcast_status('connected', '已连接 Switch')
            return
        self._auto_reconnect = True
        mac = self._get_mac()
        if not mac:
            # no MAC known - discover the Switch (it must advertise in the
            # "Change Grip/Order" menu)
            self._broadcast_status('connecting', '搜索 Switch...(请进手柄菜单)')
            found = await self._scan_for_switch()
            if not found:
                self._broadcast_status('disconnected', '未找到 Switch:请进 Switch 的『更改握法/顺序』菜单再试')
                return
            self._switch_mac, name = found
            try:
                self._save_config()
            except Exception:
                pass
            logger.info(f'found Switch: {name} ({self._switch_mac})')
            self._broadcast_status('connecting', f'找到 {name},连接中...')
        else:
            self._broadcast_status('connecting', '连接中...')
        try:
            await self._reconnect()
            self._broadcast_status('connected', '已连接 Switch')
        except Exception as e:
            logger.warning(f'connect failed: {e!r}')
            es = str(e).lower()
            # "Connection refused" = Switch still holds the slot (e.g. right
            # after a disconnect); don't scan, just let _conn_manager retry
            # after a quiet period. Other errors might mean a wrong/stale MAC,
            # so try discovering the Switch once.
            if mac and 'refused' not in es and 'errno 111' not in es:
                self._broadcast_status('connecting', '连接失败,搜索 Switch...')
                found = await self._scan_for_switch()
                if found and found[0] != mac:
                    self._switch_mac, name = found
                    try:
                        self._save_config()
                    except Exception:
                        pass
                    logger.info(f'found Switch: {name} ({self._switch_mac})')
            # auto_reconnect is on and controller_state is None, so _conn_manager
            # keeps retrying until the Switch releases its slot / becomes reachable
            self.controller_state = None
            self._broadcast_status('connecting', '重连中...')

    async def _scan_for_switch(self, timeout=8):
        # Discover nearby BR/EDR Bluetooth devices and look for a Nintendo
        # Switch. The Switch only advertises while in the "Change Grip/Order"
        # pairing menu. Returns (mac, name) or None.
        try:
            proc = await asyncio.create_subprocess_exec(
                'bluetoothctl', '--timeout', str(timeout), 'scan', 'on',
                stdout=asyncio.subprocess.PIPE, stderr=asyncio.subprocess.STDOUT)
            out, _ = await proc.communicate()
        except Exception as e:
            logger.warning(f'scan error: {e}')
            return None
        text = re.sub(r'\x1b\[[0-9;]*m', '', out.decode('utf-8', 'ignore'))
        devices = []
        for line in text.splitlines():
            m = re.search(r'\[NEW\]\s+Device\s+([0-9A-F:]{17})(?:\s+(.*))?', line, re.I)
            if m:
                devices.append((m.group(1).upper(), (m.group(2) or '').strip()))
        if not devices:
            return None
        logger.info(f'scan found {len(devices)} devices')
        for mac, name in devices:
            nl = name.lower()
            if 'nintendo' in nl or nl == 'switch' or nl.startswith('switch ') or nl == 'nx' or nl.startswith('nx '):
                return (mac, name)
        return None

    async def _disconnect(self):
        # "断开连接" button: close the BT transport and stop auto-reconnect so
        # it stays disconnected until the user connects again. Holds the lock so
        # an in-flight _reconnect can't overwrite the disconnect.
        async with self._reconnect_lock:
            self._auto_reconnect = False
            self._playing = False
            self.controller_state = None
            if self._transport is not None:
                try:
                    await self._transport.close()
                except Exception as e:
                    logger.warning(f'close transport error: {e}')
                self._transport = None
        self._broadcast_status('disconnected', '已断开')
        logger.info('disconnected from Switch')

    async def _force_reconnect(self):
        # "重连" button: close, wait a quiet period for the Switch to release
        # the controller slot, then reconnect once. Fast retries right after a
        # close either get refused (Switch holds the slot) or hang in
        # sock_connect (Switch ignores), so we pause _conn_manager during the
        # quiet window.
        self._broadcast_status('connecting', '重连中...(等待 Switch 释放)')
        async with self._reconnect_lock:
            self._auto_reconnect = False
            if self._transport is not None:
                try:
                    await self._transport.close()
                except Exception:
                    pass
                self._transport = None
            self.controller_state = None
            await asyncio.sleep(8)
            self._auto_reconnect = True
            try:
                await self._reconnect_locked()
                self._broadcast_status('connected', '已连接 Switch')
            except Exception as e:
                logger.warning(f'force reconnect failed: {e!r}')
                self.controller_state = None
                self._broadcast_status('connecting', '重连中...')

    async def _reconnect(self):
        async with self._reconnect_lock:
            await self._reconnect_locked()

    async def _reconnect_locked(self):
        # Caller must hold _reconnect_lock.
        from joycontrol.controller import Controller
        from joycontrol.memory import FlashMemory
        from joycontrol.protocol import controller_protocol_factory
        from joycontrol.server import create_hid_server
        mac = self._get_mac()
        if not mac:
            raise RuntimeError('no Switch MAC: set SWITCH_MAC or pass -r <mac>')
        spi_flash = FlashMemory()
        controller = Controller.from_arg('PRO_CONTROLLER')
        factory = controller_protocol_factory(controller, spi_flash=spi_flash)
        transport, protocol = await create_hid_server(factory, reconnect_bt_addr=mac, ctl_psm=17, itr_psm=19, capture_file=None, device_id=None)
        if self._transport is not None:
            try:
                await self._transport.close()
            except Exception:
                pass
        self._transport = transport
        self.controller_state = protocol.get_controller_state()
        try:
            await self.controller_state.connect()
        except Exception:
            # connect() failed - don't leave a half-baked controller_state
            try:
                await transport.close()
            except Exception:
                pass
            self._transport = None
            self.controller_state = None
            raise
        self._auto_reconnect = True
        logger.info('reconnected to Switch')

    async def _play_list(self, macros, loops=1, macroInterval=0, loopInterval=0):
        if not macros:
            return
        self._playing = True
        loop_count = 0
        while self._playing and (loops == 0 or loop_count < loops):
            loop_count += 1
            for idx, macro in enumerate(macros):
                if not self._playing or not macro:
                    break
                t0 = macro[0]['t'] / 1000.0
                start = time.time()
                for i, e in enumerate(macro):
                    if not self._playing:
                        break
                    target = e['t'] / 1000.0 - t0
                    now = time.time() - start
                    if target > now:
                        remaining = target - now
                        if remaining > 0.003:
                            await self._interruptible_sleep(remaining - 0.003)
                        if not self._playing:
                            break
                        while time.time() - start < target:
                            pass
                    if not self._playing:
                        break
                    await self._apply(e['ev'])
                if self._playing and macroInterval > 0 and idx < len(macros) - 1:
                    slept = 0
                    while self._playing and slept < macroInterval:
                        await asyncio.sleep(0.1)
                        slept += 0.1
            if self._playing and loopInterval > 0 and (loops == 0 or loop_count < loops):
                slept = 0
                while self._playing and slept < loopInterval:
                    await asyncio.sleep(0.1)
                    slept += 0.1
        self._playing = False
        logger.info('play list stopped/done')

    async def _apply(self, ev):
        cs = self.controller_state
        if cs is None:
            return  # not connected yet - drop input
        t = ev.get('type')
        try:
            if t == 'button':
                cs.button_state.set_button(ev['name'], bool(ev['pressed']))
                await cs.send()
            elif t == 'stick':
                stick = ev['stick']
                ss = cs.l_stick_state if stick == 'left' else cs.r_stick_state
                h = float(ev['h']); v = float(ev['v'])
                if abs(h) < 0.01 and abs(v) < 0.01:
                    ss.set_center()
                else:
                    cal = ss._calibration
                    h_val = cal.h_center + h * (cal.h_max_above_center if h >= 0 else cal.h_max_below_center)
                    v_val = cal.v_center + v * (cal.v_max_above_center if v >= 0 else cal.v_max_below_center)
                    ss.set_h(int(max(0, min(4095, h_val))))
                    ss.set_v(int(max(0, min(4095, v_val))))
        except Exception as e:
            logger.warning(f'apply error: {e} ev={ev}')


def main():
    logging.basicConfig(level=logging.INFO, format='[%(asctime)s] %(levelname)s %(message)s')
    logger.info('web_ui starting (standalone)...')
    asyncio.run(web_ui().run())


if __name__ == '__main__':
    main()

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
        self._play_gen = 0  # bumped on each play/play_list; stale tasks bail by gen
        self._pro2_enabled = False
        self._recording = False
        self._reconnect_lock = asyncio.Lock()
        self._loop = None
        self._switch_mac = ''
        self._clients = set()
        self._transport = None
        self._auto_reconnect = True
        self._last_state_bcast = 0
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
                await asyncio.wait_for(self.controller_state.send(), timeout=2)
                fail = 0
            except asyncio.CancelledError:
                raise
            except asyncio.TimeoutError:
                fail += 10  # send hung (report loop dead) - force reconnect
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
                        if self._recording:
                            # don't pollute a recording with playback events
                            logger.info('play ignored: recording in progress')
                        else:
                            self._playing = False
                            self._play_gen += 1
                            gen = self._play_gen
                            asyncio.create_task(self._play(ev.get('macro', []), ev.get('loops', 1), ev.get('interval', 0), gen))
                    elif ev.get('type') == 'play_list':
                        if self._recording:
                            # don't pollute a recording with playback events
                            logger.info('play_list ignored: recording in progress')
                        else:
                            self._playing = False
                            self._play_gen += 1
                            gen = self._play_gen
                            asyncio.create_task(self._play_list(
                                ev.get('macros', []), ev.get('loops', 1),
                                ev.get('macroInterval', 0), ev.get('loopInterval', 0),
                                ev.get('extraMacros'), ev.get('extraEveryLoops', 0),
                                ev.get('extraAfterSec', 0), ev.get('extraMacroInterval', 0), gen))
                    elif ev.get('type') == 'stop':
                        self._playing = False
                        self._play_gen += 1  # invalidate any in-flight task too
                        # Reset the controller to neutral (stop = release all
                        # buttons + center sticks) so a mid-macro stop doesn't
                        # leave a button held. No sleep needed: any playing task
                        # checks gen/playing before its next event and its
                        # in-flight send lands before this release (FIFO), so
                        # the release is always the last write and new presses
                        # right after stop are NOT swallowed.
                        await self._release_all()
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
                    elif ev.get('type') == 'rec':
                        self._recording = bool(ev.get('enabled'))
                        logger.info(f'recording={self._recording}')
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
                        # sync the virtual controller visuals on page load
                        await self._broadcast_state(force=True)
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

    def _get_state_view(self):
        # Snapshot of what is actually being sent to the Switch: pressed
        # buttons + normalized stick positions. Used for the web UI virtual
        # controller to mirror live input and macro playback.
        cs = self.controller_state
        if cs is None:
            return None
        bs = cs.button_state
        buttons = {}
        for name in bs._available_buttons:
            try:
                buttons[name] = bool(getattr(bs, name + '_is_set')())
            except Exception:
                pass
        sticks = {}
        for side, ss in (('left', cs.l_stick_state), ('right', cs.r_stick_state)):
            h = v = 0.0
            if ss is not None and ss._calibration is not None:
                cal = ss._calibration
                hr = ss.get_h(); vr = ss.get_v()
                h = (hr - cal.h_center) / (cal.h_max_above_center if hr >= cal.h_center else cal.h_max_below_center)
                v = (vr - cal.v_center) / (cal.v_max_above_center if vr >= cal.v_center else cal.v_max_below_center)
                h = max(-1.0, min(1.0, h)); v = max(-1.0, min(1.0, v))
            sticks[side] = {'h': h, 'v': v}
        return {'buttons': buttons, 'sticks': sticks}

    async def _broadcast_state(self, force=False):
        # Mirror live controller state to the web UI (button highlight + stick
        # position) so macro playback is visible. Throttled for dense stick
        # events; buttons pass force=True for instant highlight.
        now = time.time()
        if not force and now - self._last_state_bcast < 0.033:
            return
        self._last_state_bcast = now
        view = self._get_state_view()
        if view is None:
            return
        for ws in list(self._clients):
            try:
                await ws.send_json({'type': 'state', **view})
            except Exception:
                pass

    def _active(self, gen):
        # True only if playback is still on AND this task is the current one.
        # A new play/play_list bumps _play_gen, so a superseded task (older gen)
        # sees False and bails - this prevents a replayed macro from running
        # concurrently with the previous one. gen=None skips the gen check.
        return self._playing and (gen is None or self._play_gen == gen)

    async def _interruptible_sleep(self, seconds, gen=None):
        # Sleep in small chunks so 'stop' interrupts long macro gaps promptly.
        # A single long asyncio.sleep() is not interruptible - stop would wait
        # for the whole gap (macros can have 10s+ pauses) before taking effect.
        # Uses real elapsed time (not a counter) so intervals are accurate.
        end = time.time() + seconds
        while self._active(gen) and time.time() < end:
            await asyncio.sleep(min(0.05, max(0, end - time.time())))

    async def _release_all(self):
        # Release every button + center sticks, so each macro/loop iteration
        # starts from a clean state (no stuck presses eating the next press).
        cs = self.controller_state
        if cs is None:
            return
        try:
            for name in ('a', 'b', 'x', 'y', 'l', 'r', 'zl', 'zr', 'plus', 'minus',
                         'home', 'capture', 'up', 'down', 'left', 'right', 'l_stick', 'r_stick'):
                cs.button_state.set_button(name, False)
            cs.l_stick_state.set_center()
            cs.r_stick_state.set_center()
            await cs.send()
            await self._broadcast_state(force=True)
        except Exception as e:
            logger.warning(f'release_all error: {e}')

    async def _play(self, macro, loops=1, interval=0, gen=None):
        if not macro:
            logger.info('play: empty macro')
            return
        logger.info(f'playing {len(macro)} events, loops={loops}, interval={interval}s')
        self._playing = True
        loop_count = 0
        while self._active(gen) and (loops == 0 or loop_count < loops):
            loop_count += 1
            await self._release_all()
            t0 = macro[0]['t'] / 1000.0
            start = time.time()
            last_stick_t = {'left': -1000, 'right': -1000}
            for i, e in enumerate(macro):
                if not self._active(gen):
                    break
                target = e['t'] / 1000.0 - t0
                now = time.time() - start
                if target > now:
                    await self._interruptible_sleep(target - now, gen)
                    if not self._active(gen):
                        break
                if not self._active(gen):
                    break
                if e['ev'].get('type') == 'stick':
                    # Keep events up to ~100Hz (incl. old 66Hz-recorded macros,
                    # whose 15ms gaps a 16ms threshold dropped every other one,
                    # distorting the stick trajectory / leaving it off-center).
                    # Only drop denser-than-100Hz stick events, which the Switch
                    # can't track.
                    if e['t'] - last_stick_t[e['ev']['stick']] < 10:
                        continue
                    last_stick_t[e['ev']['stick']] = e['t']
                await self._apply(e['ev'])
            if self._active(gen):
                # each loop ends neutral before the interval / next loop
                await self._release_all()
            if self._active(gen) and interval > 0 and (loops == 0 or loop_count < loops):
                await self._interruptible_sleep(interval, gen)
        # Only clear _playing if we're still the current task - a superseded
        # task must not clobber the flag the newer task now owns.
        if gen is None or self._play_gen == gen:
            # Macro finished - return the controller to neutral (release all
            # buttons + center sticks) so it doesn't hold the last input.
            await self._release_all()
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
        lastRecStick = {'left': (0.0, 0.0), 'right': (0.0, 0.0)}
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
                                ev = {'type': 'button', 'name': name, 'pressed': bool(e.value)}
                                self._apply_ts(ev)
                                if self._recording:
                                    self._broadcast_record(ev)
                        elif e.type == 3:
                            if e.code in STICK_AXIS:
                                side, axis = STICK_AXIS[e.code]
                                nv = e.value / 32767.0
                                if abs(nv) < 0.1:
                                    nv = 0.0
                                if axis == 'v':
                                    nv = -nv
                                stick[side][axis] = nv
                                ev = {'type': 'stick', 'stick': side, 'h': stick[side]['h'], 'v': stick[side]['v']}
                                self._apply_ts(ev)
                                if self._recording:
                                    h, v = stick[side]['h'], stick[side]['v']
                                    lh, lv = lastRecStick[side]
                                    if abs(h - lh) > 0.05 or abs(v - lv) > 0.05:
                                        lastRecStick[side] = (h, v)
                                        self._broadcast_record(ev)
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

    def _broadcast_record(self, ev):
        # From the NS reader thread: send a Pro2 input event to the web UI so it
        # can be recorded into the macro (Pro2 input bypasses the frontend, so
        # it would otherwise never be captured).
        try:
            asyncio.run_coroutine_threadsafe(self._broadcast_record_coro(ev), self._loop)
        except Exception as e:
            logger.warning(f'record dispatch error: {e}')

    async def _broadcast_record_coro(self, ev):
        msg = {'type': 'record', 'ev': ev}
        for ws in list(self._clients):
            try:
                await ws.send_json(msg)
            except Exception:
                pass

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
            if 'refused' in es or 'errno 111' in es:
                # Switch still holds the controller slot - release it, then retry
                self._broadcast_status('connecting', '释放 Switch 连接槽...')
                await self._release_switch()
                await asyncio.sleep(2)
                try:
                    await self._reconnect()
                    self._broadcast_status('connected', '已连接 Switch')
                    return
                except Exception as e2:
                    logger.warning(f'reconnect after release failed: {e2!r}')
            elif mac:
                # maybe a wrong/stale MAC - try discovering the Switch once
                self._broadcast_status('connecting', '连接失败,搜索 Switch...')
                found = await self._scan_for_switch()
                if found and found[0] != mac:
                    self._switch_mac, name = found
                    try:
                        self._save_config()
                    except Exception:
                        pass
                    logger.info(f'found Switch: {name} ({self._switch_mac})')
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

    async def _release_switch(self):
        # Force the Switch to free its controller slot by resetting the BT
        # adapter. After a drop the Switch holds the slot for ~8s and refuses a
        # new L2CAP ("Connection refused"); resetting hci0 clears it immediately
        # so disconnect/reconnect work without the wait (or a manual reset).
        try:
            proc = await asyncio.create_subprocess_exec(
                'hciconfig', 'hci0', 'reset',
                stdout=asyncio.subprocess.DEVNULL, stderr=asyncio.subprocess.DEVNULL)
            await proc.communicate()
            logger.info('BT adapter reset - released Switch controller slot')
        except Exception as e:
            logger.warning(f'bt reset error: {e}')

    async def _disconnect(self):
        # "断开连接" button: close the BT transport, release the Switch's
        # controller slot, and stop auto-reconnect. Holds the lock so an
        # in-flight _reconnect can't overwrite the disconnect.
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
        await self._release_switch()
        self._broadcast_status('disconnected', '已断开')
        logger.info('disconnected from Switch')

    async def _force_reconnect(self):
        # "重连" button: close the connection, release the Switch's slot (BT
        # reset), then reconnect - no need to wait out the Switch's ~8s hold.
        self._broadcast_status('connecting', '重连中...')
        async with self._reconnect_lock:
            self._auto_reconnect = False
            if self._transport is not None:
                try:
                    await self._transport.close()
                except Exception:
                    pass
                self._transport = None
            self.controller_state = None
            await self._release_switch()
            await asyncio.sleep(2)  # let the adapter come back up after reset
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
            # connect() waits on sig_set_player_lights, which the Switch only
            # sends when the pairing handshake completes. If the connection is
            # reset mid-handshake (e.g. it dropped during a previous session),
            # that event never fires and connect() would hang forever, holding
            # _reconnect_lock and freezing the conn_manager. Timeout it so the
            # caller closes the transport and retries.
            await asyncio.wait_for(self.controller_state.connect(), timeout=15)
        except Exception:
            # connect() failed/timeout - don't leave a half-baked controller_state
            try:
                await transport.close()
            except Exception:
                pass
            self._transport = None
            self.controller_state = None
            raise
        self._auto_reconnect = True
        logger.info('reconnected to Switch')

    async def _play_list_once(self, macros, macroInterval, gen=None):
        # Play one pass through a macro list (all macros, in order). Shared by
        # the main list loop and the extra (inserted) macro list. Checks
        # _active(gen) throughout so 'stop' or a superseding play interrupts.
        for idx, macro in enumerate(macros):
            if not self._active(gen) or not macro:
                break
            await self._release_all()
            t0 = macro[0]['t'] / 1000.0
            start = time.time()
            last_stick_t = {'left': -1000, 'right': -1000}
            for i, e in enumerate(macro):
                if not self._active(gen):
                    break
                target = e['t'] / 1000.0 - t0
                now = time.time() - start
                if target > now:
                    await self._interruptible_sleep(target - now, gen)
                    if not self._active(gen):
                        break
                if not self._active(gen):
                    break
                if e['ev'].get('type') == 'stick':
                    # Keep events up to ~100Hz (incl. old 66Hz macros); only
                    # drop denser-than-100Hz stick events (see _play).
                    if e['t'] - last_stick_t[e['ev']['stick']] < 10:
                        continue
                    last_stick_t[e['ev']['stick']] = e['t']
                await self._apply(e['ev'])
            if self._active(gen) and macroInterval > 0 and idx < len(macros) - 1:
                await self._interruptible_sleep(macroInterval, gen)

    async def _play_list(self, macros, loops=1, macroInterval=0, loopInterval=0,
                         extraMacros=None, extraEveryLoops=0, extraAfterSec=0,
                         extraMacroInterval=0, gen=None):
        if not macros:
            return
        self._playing = True
        loop_count = 0
        # Extra-list trigger state: count main iterations since the last extra
        # run, and track the time baseline. Either condition (count OR elapsed
        # time) fires the extra list at the end of a completed main iteration;
        # both reset after the extra runs, so it repeats periodically.
        extra_since = 0
        extra_t0 = time.time()
        while self._active(gen) and (loops == 0 or loop_count < loops):
            loop_count += 1
            extra_since += 1
            await self._play_list_once(macros, macroInterval, gen)
            # After a full main-list pass, maybe insert the extra list - runs
            # right at the end of "that round", before the loopInterval pause.
            if self._active(gen) and extraMacros:
                fire = False
                if extraEveryLoops > 0 and extra_since >= extraEveryLoops:
                    fire = True
                if extraAfterSec > 0 and (time.time() - extra_t0) >= extraAfterSec:
                    fire = True
                if fire:
                    logger.info(f'extra macro list inserted after loop {loop_count}')
                    await self._play_list_once(extraMacros, extraMacroInterval, gen)
                    extra_since = 0
                    extra_t0 = time.time()
            if self._active(gen):
                # each list round ends neutral before the loop interval / next round
                await self._release_all()
            if self._active(gen) and loopInterval > 0 and (loops == 0 or loop_count < loops):
                await self._interruptible_sleep(loopInterval, gen)
        # Only clear _playing if we're still the current task - a superseded
        # task must not clobber the flag the newer task now owns.
        if gen is None or self._play_gen == gen:
            # List finished - return the controller to neutral (release all
            # buttons + center sticks) so it doesn't hold the last input.
            await self._release_all()
            self._playing = False
        logger.info('play list stopped/done')

    async def _apply(self, ev):
        cs = self.controller_state
        if cs is None:
            return  # not connected yet - drop input
        t = ev.get('type')
        try:
            centered = False
            if t == 'button':
                cs.button_state.set_button(ev['name'], bool(ev['pressed']))
                await cs.send()
            elif t == 'stick':
                stick = ev['stick']
                ss = cs.l_stick_state if stick == 'left' else cs.r_stick_state
                h = float(ev['h']); v = float(ev['v'])
                centered = abs(h) < 0.01 and abs(v) < 0.01
                if centered:
                    ss.set_center()
                else:
                    cal = ss._calibration
                    h_val = cal.h_center + h * (cal.h_max_above_center if h >= 0 else cal.h_max_below_center)
                    v_val = cal.v_center + v * (cal.v_max_above_center if v >= 0 else cal.v_max_below_center)
                    ss.set_h(int(max(0, min(4095, h_val))))
                    ss.set_v(int(max(0, min(4095, v_val))))
            # force-broadcast buttons and stick->center so the web UI knob always
            # returns to center (a throttled broadcast can drop the center event)
            await self._broadcast_state(force=(t == 'button' or centered))
        except Exception as e:
            logger.warning(f'apply error: {e} ev={ev}')


def main():
    logging.basicConfig(level=logging.INFO, format='[%(asctime)s] %(levelname)s %(message)s')
    logger.info('web_ui starting (standalone)...')
    asyncio.run(web_ui().run())


if __name__ == '__main__':
    main()

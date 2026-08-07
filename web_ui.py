import asyncio
import json
import logging
import os
from aiohttp import web, WSMsgType
from JoycontrolPlugin import JoycontrolPlugin

logger = logging.getLogger(__name__)

WEB_DIR = os.path.join(os.path.dirname(os.path.abspath(__file__)), 'web')


class web_ui(JoycontrolPlugin):
    async def run(self):
        self._playing = False
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
        asyncio.create_task(self._stick_sender())
        await asyncio.Event().wait()

    async def _index(self, request):
        return web.FileResponse(os.path.join(WEB_DIR, 'index.html'))

    async def _ws_handler(self, request):
        ws = web.WebSocketResponse()
        await ws.prepare(request)
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
                    else:
                        await self._apply(ev)
                except Exception as e:
                    logger.warning(f'ws apply error: {e}')
            elif msg.type == WSMsgType.ERROR:
                logger.warning(f'ws error: {ws.exception()}')
        return ws

    async def _play(self, macro, loops=1, interval=0):
        if not macro:
            logger.info('play: empty macro')
            return
        logger.info(f'playing {len(macro)} events, loops={loops}, interval={interval}s')
        import time
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
                        await asyncio.sleep(remaining - 0.003)
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

    async def _stick_sender(self):
        while True:
            await asyncio.sleep(1 / 60)
            try:
                await self.controller_state.send()
            except Exception:
                pass

    async def _play_list(self, macros, loops=1, macroInterval=0, loopInterval=0):
        if not macros:
            return
        import time
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
                            await asyncio.sleep(remaining - 0.003)
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

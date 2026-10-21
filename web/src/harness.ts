// A stand in for the native app during development: it loads the game in an
// iframe sized like a phone, draws the hardware cutouts, sends the safe area
// insets the way an Android host would, and logs bridge traffic both ways.

const DEVICES = {
  iphone: { w: 393, h: 852, top: 59, bottom: 34, notch: 126 },
  pixel: { w: 412, h: 915, top: 32, bottom: 24, notch: 24 },
  small: { w: 360, h: 640, top: 24, bottom: 0, notch: 0 },
  tablet: { w: 768, h: 1024, top: 24, bottom: 20, notch: 0 },
} as const;

const frame = document.getElementById('frame') as HTMLIFrameElement;
const log = document.getElementById('log')!;
const select = document.getElementById('device') as HTMLSelectElement;

function write(dir: string, msg: unknown) {
  const line = `${new Date().toLocaleTimeString()} ${dir} ${JSON.stringify(msg)}\n`;
  log.textContent = line + (log.textContent ?? '');
}

function send(msg: Record<string, unknown>) {
  frame.contentWindow?.postMessage({ source: 'pocket-arcade-host', ...msg }, '*');
  write('host →', msg);
}

function applyDevice() {
  const d = DEVICES[select.value as keyof typeof DEVICES];
  frame.width = String(d.w);
  frame.height = String(d.h);
  const notch = document.getElementById('notch')!;
  Object.assign(notch.style, { top: '0', width: `${d.notch}px`, height: `${d.notch ? 34 : 0}px`, borderRadius: '0 0 18px 18px' });
  const bar = document.getElementById('bar')!;
  Object.assign(bar.style, { bottom: '8px', width: `${d.bottom ? 134 : 0}px`, height: '5px', background: '#9ca3af', borderRadius: '3px' });
  send({ type: 'safeArea', top: d.top, right: 0, bottom: d.bottom, left: 0 });
}

window.addEventListener('message', (ev) => {
  if (ev.source === frame.contentWindow && ev.data?.source === 'pocket-arcade') {
    const { source: _source, ...msg } = ev.data;
    write('← page', msg);
    if (msg.type === 'ready') applyDevice(); // a real host sends insets as soon as the page is ready
  }
});

select.addEventListener('change', applyDevice);
for (const b of document.querySelectorAll<HTMLButtonElement>('[data-send]')) {
  b.addEventListener('click', () => send(JSON.parse(b.dataset.send!)));
}
document.getElementById('player')!.addEventListener('click', () =>
  send({ type: 'player', id: 'harness-user-01', name: 'Harness Hana' }),
);
applyDevice();

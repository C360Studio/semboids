// Node 22 native WebSocket observer; no package install or application changes.
const ws = new WebSocket(process.argv[2]);
ws.onopen = () => console.error('connected');
ws.onmessage = ({data}) => {
  const envelope = JSON.parse(String(data));
  const frame = envelope.payload ?? envelope;
  if (typeof frame.tick === 'number' && Array.isArray(frame.boids)) {
    console.log(JSON.stringify({at: Date.now() / 1000, tick: frame.tick,
      population: frame.boids.length, frame_timestamp_ms: frame.t}));
  }
};
ws.onerror = (event) => console.error('websocket error', event.message ?? 'unknown');
ws.onclose = ({code, reason}) => { console.error('closed', code, reason); process.exit(0); };
process.on('SIGTERM', () => { ws.close(1000); setTimeout(() => process.exit(0), 1000).unref(); });

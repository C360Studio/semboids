// Node 22 observer mirrors the UI's first 500ms reconnect delay.
let socket;
let stopping = false;
let retry;
const log = (...fields) => console.error(new Date().toISOString(), ...fields);
function connect() {
  if (stopping) return;
  socket = new WebSocket(process.argv[2]);
  socket.onopen = () => log('connected');
  socket.onmessage = ({data}) => {
    const envelope = JSON.parse(String(data));
    const frame = envelope.payload ?? envelope;
    if (typeof frame.tick === 'number' && Array.isArray(frame.boids)) {
      console.log(JSON.stringify({at: Date.now() / 1000, tick: frame.tick,
        population: frame.boids.length, frame_timestamp_ms: frame.t}));
    }
  };
  socket.onerror = (event) => log('websocket error', event.message ?? 'unknown');
  socket.onclose = ({code, reason}) => {
    log('closed', code, reason);
    if (stopping) process.exit(0);
    retry = setTimeout(connect, 500);
  };
}
connect();
process.on('SIGTERM', () => {
  stopping = true;
  clearTimeout(retry);
  socket?.close(1000);
  setTimeout(() => process.exit(0), 1000).unref();
});

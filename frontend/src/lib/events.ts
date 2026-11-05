import { Events } from "@wailsio/runtime";
import { KupiChanged, KupiLogs, KupiShell, type Changed, type LogBatch, type ShellChunk } from "./api";

type Unsubscribe = () => void;

function subscribe<T extends { id: string }>(name: string, id: string, handler: (data: T) => void): Unsubscribe {
  const off = Events.On(name, (event: { data: unknown }) => {
    const data = event.data as T;
    if (data && data.id === id) handler(data);
  });
  return typeof off === "function" ? off : () => Events.Off(name);
}

/** Calls handler when the watched list with this id changes. */
export function onChanged(id: string, handler: (data: Changed) => void): Unsubscribe {
  return subscribe<Changed>(KupiChanged, id, handler);
}

/** Calls handler with each batch of log lines for this stream. */
export function onLogs(id: string, handler: (data: LogBatch) => void): Unsubscribe {
  return subscribe<LogBatch>(KupiLogs, id, handler);
}

/** Calls handler with terminal output for this shell session. */
export function onShell(id: string, handler: (data: ShellChunk) => void): Unsubscribe {
  return subscribe<ShellChunk>(KupiShell, id, handler);
}

/**
 * Listens before the stream id is known. A stream can send data before the
 * call that started it returns, so events are buffered until bind(id).
 */
export function bufferedListener<T extends { id: string }>(name: string, handler: (data: T) => void) {
  let id = "";
  const pending: T[] = [];
  const off = Events.On(name, (event: { data: unknown }) => {
    const data = event.data as T;
    if (!data) return;
    if (!id) {
      pending.push(data);
      if (pending.length > 2000) pending.shift();
      return;
    }
    if (data.id === id) handler(data);
  });
  return {
    bind(streamId: string) {
      id = streamId;
      for (const data of pending.splice(0)) if (data.id === id) handler(data);
    },
    off() {
      if (typeof off === "function") off();
    },
  };
}

export { KupiLogs, KupiShell };

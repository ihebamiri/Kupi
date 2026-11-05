import { useEffect, useState } from "react";
import { Events } from "@wailsio/runtime";
import { ForwardService, KupiForwards, type Forward, type ForwardsChanged } from "../lib/api";

/** Live list of port-forwards, updated by the backend's "kupi:forwards" event. */
export function useForwards(): Forward[] {
  const [forwards, setForwards] = useState<Forward[]>([]);
  useEffect(() => {
    let cancelled = false;
    ForwardService.List()
      .then((list) => !cancelled && setForwards(list ?? []))
      .catch(() => {});
    const off = Events.On(KupiForwards, (event: { data: unknown }) => {
      const data = event.data as ForwardsChanged;
      if (!cancelled) setForwards(data?.forwards ?? []);
    });
    return () => {
      cancelled = true;
      if (typeof off === "function") off();
    };
  }, []);
  return forwards;
}

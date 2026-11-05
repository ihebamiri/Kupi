export type ProfileKind = "local" | "dev" | "staging" | "production";

export const PROFILE_KINDS: ProfileKind[] = ["local", "dev", "staging", "production"];

interface ProfileMeta {
  label: string;
  /** Tailwind classes for a badge in this profile's colour. */
  badge: string;
  /** Tailwind class for the coloured top border of the context bar. */
  border: string;
  description: string;
}

const META: Record<ProfileKind, ProfileMeta> = {
  local: {
    label: "Local",
    badge: "bg-local/15 text-local",
    border: "border-local",
    description: "A cluster on this computer. Changes apply after a quick diff.",
  },
  dev: {
    label: "Dev",
    badge: "bg-dev/15 text-dev",
    border: "border-dev",
    description: "A shared development cluster. Changes apply after review.",
  },
  staging: {
    label: "Staging",
    badge: "bg-staging/15 text-staging",
    border: "border-staging",
    description: "A pre-production cluster. Changes apply after review.",
  },
  production: {
    label: "Production",
    badge: "bg-production/15 text-production",
    border: "border-production",
    description: "Opens read-only. Changes need editing switched on, a reason, and typed confirmation for deletes.",
  },
};

export function isProfileKind(value: string): value is ProfileKind {
  return (PROFILE_KINDS as string[]).includes(value);
}

/** Unknown values get production treatment, matching the backend. */
export function profileMeta(kind: string): ProfileMeta {
  return isProfileKind(kind) ? META[kind] : META.production;
}

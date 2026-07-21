import type { UserAvailableChannel } from '@/api/channels'

export function radarCatalogPlatforms(channels: readonly UserAvailableChannel[]): string[] {
  const platforms = new Set<string>()
  for (const channel of channels) {
    for (const section of channel.platforms ?? []) {
      const platform = section.platform.trim().toLowerCase()
      if (platform && (section.supported_models?.length ?? 0) > 0) platforms.add(platform)
    }
  }
  return [...platforms].sort((left, right) => left.localeCompare(right))
}

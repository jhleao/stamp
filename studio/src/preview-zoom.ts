export function clampZoom(value: number): number {
  return Math.max(50, Math.min(200, value));
}

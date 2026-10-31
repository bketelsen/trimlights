export function clockTime(value: string) {
  const match = /^(\d{2}):(\d{2})$/.exec(value);
  if (!match) return value;
  const date = new Date(2000, 0, 1, Number(match[1]), Number(match[2]));
  return date.toLocaleTimeString(undefined, { hour: 'numeric', minute: '2-digit' });
}
export function repeatLabel(value: string) {
  if (value.startsWith('every day')) return 'Every day';
  if (value.startsWith('today only')) return 'One day only';
  return 'Custom repeat';
}

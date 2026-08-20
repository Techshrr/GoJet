export function assertNever(value: never): never {
  throw new Error(`Unexpected value: ${String(value)}`);
}

export { SHELL_STATES, isShellState } from './shell-states';
export type { ShellState, ShellSurface } from './shell-states';

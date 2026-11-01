export declare class FormatReason {
  static SHORT: Readonly<Record<string, string>>;
  static raw(reason: string | null | undefined, eventFallback?: string | null | undefined): string;
  static label(reason: string | null | undefined, eventFallback?: string | null | undefined): string;
  static tidy(raw: string): string;
}

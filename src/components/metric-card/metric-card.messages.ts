export const Messages = {
  loadFailed: (reason: string) => `Failed to load metrics: ${reason}`,
  noData: 'No data for this time range',
  perSecond: (value: string) => `${value}/s`,
  cores: (value: string) => `${value} cores`,
  milliseconds: (value: string) => `${value} ms`,
  seconds: (value: string) => `${value} s`,
};

import { Box, Stack, Typography } from '@mui/material';
import { MetricUnit } from 'types/metrics.types';
import { ChartSeries, formatValue } from '../metric-card.utils';

interface SeriesLegendProps {
  series: ChartSeries[];
  unit: MetricUnit;
}

export const SeriesLegend = ({ series, unit }: SeriesLegendProps) => (
  <Stack direction="row" sx={{ flexWrap: 'wrap', columnGap: 2.5, rowGap: 0.5 }}>
    {series.map((s) => (
      <Stack key={s.label} direction="row" sx={{ alignItems: 'center', gap: 0.75 }}>
        <Box sx={{ width: 8, height: 8, borderRadius: '50%', bgcolor: s.color }} />
        <Typography variant="caption" color="text.secondary">
          {s.label}
        </Typography>
        {s.latest !== null && (
          <Typography variant="caption" sx={{ fontWeight: 600, fontVariantNumeric: 'tabular-nums' }}>
            {formatValue(s.latest, unit)}
          </Typography>
        )}
      </Stack>
    ))}
  </Stack>
);

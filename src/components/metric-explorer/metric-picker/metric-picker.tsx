import { useMemo, useState } from 'react';
import { Autocomplete, createFilterOptions, InputAdornment, Stack, TextField, Typography } from '@mui/material';
import SearchIcon from '@mui/icons-material/Search';
import { MetricInfo } from 'types/metrics.types';
import { Messages } from '../metric-explorer.messages';
import { PickerOption, toPickerOptions } from '../metric-explorer.utils';

interface MetricPickerProps {
  metrics: MetricInfo[];
  picked: MetricInfo[];
  isLoading: boolean;
  error: Error | null;
  onPick: (metric: MetricInfo) => void;
}

const filterOptions = createFilterOptions<PickerOption>({
  stringify: ({ metric }) => `${metric.name} ${metric.help ?? ''}`,
});

export const MetricPicker = ({ metrics, picked, isLoading, error, onPick }: MetricPickerProps) => {
  const [input, setInput] = useState('');
  const options = useMemo(() => toPickerOptions(metrics, picked), [metrics, picked]);

  return (
    <Autocomplete
      size="small"
      options={options}
      value={null}
      inputValue={input}
      onInputChange={(_, value, reason) => setInput(reason === 'input' ? value : '')}
      onChange={(_, option) => option && onPick(option.metric)}
      groupBy={(option) => option.group}
      getOptionLabel={(option) => option.metric.name}
      isOptionEqualToValue={(a, b) => a.metric.name === b.metric.name}
      filterOptions={filterOptions}
      loading={isLoading}
      loadingText={Messages.loading}
      noOptionsText={Messages.noOptions}
      sx={{ maxWidth: 560 }}
      renderOption={({ key, ...props }, { metric }) => (
        <li key={key} {...props}>
          <Stack sx={{ minWidth: 0 }}>
            <Typography variant="body2" sx={{ fontFamily: 'monospace' }} noWrap>
              {metric.name}
            </Typography>
            <Typography variant="caption" color="text.secondary" noWrap>
              {Messages.caption(metric.type, metric.help)}
            </Typography>
          </Stack>
        </li>
      )}
      renderInput={(params) => (
        <TextField
          {...params}
          placeholder={Messages.placeholder}
          error={!!error}
          helperText={error ? Messages.loadFailed(error.message) : undefined}
          slotProps={{
            input: {
              ...params.InputProps,
              startAdornment: (
                <InputAdornment position="start">
                  <SearchIcon fontSize="small" />
                </InputAdornment>
              ),
            },
          }}
        />
      )}
    />
  );
};

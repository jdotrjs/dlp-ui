import { h } from 'preact';

// MaxConcurrentField caps simultaneous downloads (>= 1).
interface MaxConcurrentFieldProps {
  value: number;
  onChange: (value: number) => void;
}

export function MaxConcurrentField(props: MaxConcurrentFieldProps) {
  return (
    <div class="field">
      <label class="field-label">Max concurrent downloads</label>
      <input
        class="number-input"
        type="number"
        min={1}
        max={16}
        value={props.value}
        onInput={(e) => {
          const n = parseInt((e.target as HTMLInputElement).value, 10);
          props.onChange(Number.isFinite(n) && n >= 1 ? n : 1);
        }}
      />
    </div>
  );
}

import { h } from 'preact';

// RestrictNamesToggle controls the --restrict-filenames flag (ASCII-only,
// spaces -> underscores).
interface RestrictNamesToggleProps {
  value: boolean;
  onChange: (value: boolean) => void;
}

export function RestrictNamesToggle(props: RestrictNamesToggleProps) {
  return (
    <div class="field">
      <label class="checkbox-row">
        <input
          type="checkbox"
          checked={props.value}
          onChange={(e) => props.onChange((e.target as HTMLInputElement).checked)}
        />
        <span>Restrict filenames (ASCII-only, no spaces or special characters)</span>
      </label>
    </div>
  );
}

import { h } from 'preact';
import { PickDirectory } from '../api/client';

// PathField is a labelled text input with a Browse button that opens a native
// directory picker (PickDirectory). Used for the download directory.
interface PathFieldProps {
  label: string;
  hint?: string;
  value: string;
  placeholder?: string;
  onChange: (value: string) => void;
}

export function PathField(props: PathFieldProps) {
  async function browse() {
    const picked = await PickDirectory(props.label);
    if (picked) props.onChange(picked);
  }

  return (
    <div class="field">
      <label class="field-label">{props.label}</label>
      {props.hint && <div class="field-hint">{props.hint}</div>}
      <div class="field-row">
        <input
          class="text-input"
          type="text"
          value={props.value}
          placeholder={props.placeholder}
          onInput={(e) => props.onChange((e.target as HTMLInputElement).value)}
        />
        <button class="btn" onClick={browse}>
          Browse…
        </button>
      </div>
    </div>
  );
}

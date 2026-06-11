import { h } from 'preact';
import { exampleFromTemplate } from '../lib/format';

// TemplateField edits a yt-dlp output template and shows a live example of the
// resulting filename, accounting for the restrict-filenames toggle.
interface TemplateFieldProps {
  label: string;
  hint?: string;
  value: string;
  restrict: boolean;
  placeholder?: string;
  onChange: (value: string) => void;
}

export function TemplateField(props: TemplateFieldProps) {
  const example = exampleFromTemplate(props.value, props.restrict);
  return (
    <div class="field">
      <label class="field-label">{props.label}</label>
      {props.hint && <div class="field-hint">{props.hint}</div>}
      <input
        class="text-input"
        type="text"
        value={props.value}
        placeholder={props.placeholder}
        onInput={(e) => props.onChange((e.target as HTMLInputElement).value)}
      />
      {example && (
        <div class="field-example">
          Example: <code>{example}</code>
        </div>
      )}
    </div>
  );
}

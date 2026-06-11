import { h } from 'preact';
import { PlaylistMode, PLAYLIST_MODES } from '../lib/types';

// PlaylistModeRadio selects how playlists are laid out on disk. Choosing
// "custom" reveals a template input bound to playlistTemplate.
interface PlaylistModeRadioProps {
  mode: PlaylistMode;
  template: string;
  onModeChange: (mode: PlaylistMode) => void;
  onTemplateChange: (template: string) => void;
}

export function PlaylistModeRadio(props: PlaylistModeRadioProps) {
  return (
    <div class="field">
      <label class="field-label">Playlist layout</label>
      <div class="radio-group">
        {PLAYLIST_MODES.map((opt) => (
          <label class="radio-row" key={opt.value}>
            <input
              type="radio"
              name="playlistMode"
              checked={props.mode === opt.value}
              onChange={() => props.onModeChange(opt.value)}
            />
            <span>{opt.label}</span>
          </label>
        ))}
      </div>
      {props.mode === 'custom' && (
        <input
          class="text-input"
          type="text"
          value={props.template}
          placeholder="%(playlist)s/%(playlist_index)s-%(title)s.%(ext)s"
          onInput={(e) => props.onTemplateChange((e.target as HTMLInputElement).value)}
        />
      )}
    </div>
  );
}

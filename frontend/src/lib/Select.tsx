// import { h } from 'preact';
import ReactSelect from 'react-select';

// Themed wrapper around react-select. We use it instead of <select> because
// macOS WebKit refuses to style the native control consistently. The wrapper
// keeps the consumer API string-shaped (value/onChange take primitives) and
// portals the menu to document.body so it never gets clipped by modals or
// overflow:hidden ancestors. Styling is driven by classNamePrefix="rs"
// against the .rs__* rules in app.css.
export interface SelectOption<V extends string = string> {
  value: V;
  label: string;
  isDisabled?: boolean;
}

interface Props<V extends string> {
  value: V;
  options: ReadonlyArray<SelectOption<V>>;
  onChange: (value: V) => void;
  isDisabled?: boolean;
  isSearchable?: boolean;
  placeholder?: string;
  class?: string;
}

export function Select<V extends string = string>({
  value,
  options,
  onChange,
  isDisabled,
  isSearchable = false,
  placeholder,
  class: className,
}: Props<V>) {
  const selected = options.find((o) => o.value === value) ?? null;
  const containerClass = className ? `rs-select ${className}` : 'rs-select';
  // TODO: I can't tell if the type incompatability here is a preact thing
  // or if there is a legit error. I also can't repro it outside of VScode's
  // magic tooling. It goes away
  return (
    <ReactSelect
      unstyled
      classNamePrefix="rs"
      className={containerClass}
      menuPortalTarget={typeof document !== 'undefined' ? document.body : undefined}
      menuPlacement="auto"
      // The modal backdrop sits at z-index 50; bump portal above it via inline
      // style because `unstyled` mode bypasses any portal class we'd target in
      // CSS.
      styles={{ menuPortal: (base) => ({ ...base, zIndex: 200 }) }}
      options={options as SelectOption<V>[]}
      value={selected}
      onChange={(opt) => onChange(((opt as SelectOption<V> | null)?.value ?? value))}
      isDisabled={isDisabled}
      isSearchable={isSearchable}
      placeholder={placeholder}
    />
  );
}

type ChoiceGroupProps<T extends string> = {
  legend: string;
  name: string;
  options: readonly (readonly [value: T, label: string])[];
  value: T;
  onChange: (value: T) => void;
};

/** A labelled set of radio buttons. */
export function ChoiceGroup<T extends string>({
  legend,
  name,
  options,
  value,
  onChange,
}: ChoiceGroupProps<T>) {
  return (
    <fieldset>
      <legend className="mb-1 text-ink-2 text-ui">{legend}</legend>
      <div className="flex flex-wrap gap-x-4 gap-y-1">
        {options.map(([optionValue, label]) => (
          <label key={optionValue} className="inline-flex items-center gap-1.5 text-ui-lg">
            <input
              type="radio"
              name={name}
              value={optionValue}
              checked={optionValue === value}
              onChange={() => onChange(optionValue)}
              className="accent-accent"
            />
            {label}
          </label>
        ))}
      </div>
    </fieldset>
  );
}

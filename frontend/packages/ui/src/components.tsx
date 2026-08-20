import {
  AlertTriangle,
  CheckCircle2,
  ChevronLeft,
  ChevronRight,
  CircleAlert,
  Info,
  LoaderCircle,
  Search,
  X,
} from 'lucide-react';
import {
  type AnchorHTMLAttributes,
  type ButtonHTMLAttributes,
  type ChangeEvent,
  type HTMLAttributes,
  type InputHTMLAttributes,
  type KeyboardEvent,
  type ReactNode,
  type SelectHTMLAttributes,
  type TextareaHTMLAttributes,
  useEffect,
  useId,
  useRef,
  useState,
} from 'react';

export type FoundationSurface = 'website' | 'workspace' | 'admin' | 'docs';
export type ButtonVariant = 'primary' | 'secondary' | 'outline' | 'ghost' | 'destructive' | 'link';
export type FeedbackVariant = 'success' | 'warning' | 'danger' | 'info';

export interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: ButtonVariant;
  loading?: boolean;
  selected?: boolean;
}

export function Button({
  variant = 'secondary',
  loading = false,
  selected,
  disabled,
  children,
  type = 'button',
  ...props
}: ButtonProps) {
  return (
    <button
      {...props}
      className={['gj-button', props.className].filter(Boolean).join(' ')}
      data-variant={variant}
      type={type}
      disabled={disabled || loading}
      aria-busy={loading || undefined}
      aria-pressed={selected}
    >
      {loading ? <LoaderCircle aria-hidden="true" /> : null}
      <span>{children}</span>
      {loading ? <span className="gj-sr-only">in progress</span> : null}
    </button>
  );
}

export function InlineLink({ className, ...props }: AnchorHTMLAttributes<HTMLAnchorElement>) {
  return <a {...props} className={['gj-link', className].filter(Boolean).join(' ')} />;
}

function describedBy(ids: Array<string | undefined>) {
  const value = ids.filter(Boolean).join(' ');
  return value || undefined;
}

interface FieldMeta {
  id: string;
  label: string;
  helpText?: string;
  error?: string;
  success?: string;
}

function FieldStatus({ id, error, success }: { id: string; error?: string | undefined; success?: string | undefined }) {
  if (error) {
    return (
      <div id={`${id}--error`} className="gj-field__status" data-status="invalid" role="alert">
        <CircleAlert aria-hidden="true" />
        <span>{error}</span>
      </div>
    );
  }
  if (success) {
    return (
      <div id={`${id}--status`} className="gj-field__status" data-status="success" aria-live="polite">
        <CheckCircle2 aria-hidden="true" />
        <span>{success}</span>
      </div>
    );
  }
  return null;
}

export interface TextFieldProps
  extends FieldMeta,
    Omit<InputHTMLAttributes<HTMLInputElement>, 'id' | 'aria-invalid'> {}

export function TextField({ id, label, helpText, error, success, className, ...props }: TextFieldProps) {
  return (
    <div className="gj-field">
      <label className="gj-field__label" htmlFor={id}>{label}</label>
      <input
        {...props}
        id={id}
        className={['gj-input', className].filter(Boolean).join(' ')}
        aria-invalid={error ? true : undefined}
        aria-describedby={describedBy([
          helpText ? `${id}--help` : undefined,
          error ? `${id}--error` : undefined,
          success ? `${id}--status` : undefined,
        ])}
        data-success={success ? 'true' : undefined}
      />
      {helpText ? <div id={`${id}--help`} className="gj-field__help">{helpText}</div> : null}
      <FieldStatus id={id} error={error} success={success} />
    </div>
  );
}

export interface TextareaFieldProps
  extends FieldMeta,
    Omit<TextareaHTMLAttributes<HTMLTextAreaElement>, 'id' | 'aria-invalid'> {}

export function TextareaField({ id, label, helpText, error, success, className, ...props }: TextareaFieldProps) {
  return (
    <div className="gj-field">
      <label className="gj-field__label" htmlFor={id}>{label}</label>
      <textarea
        {...props}
        id={id}
        className={['gj-textarea', className].filter(Boolean).join(' ')}
        aria-invalid={error ? true : undefined}
        aria-describedby={describedBy([
          helpText ? `${id}--help` : undefined,
          error ? `${id}--error` : undefined,
          success ? `${id}--status` : undefined,
        ])}
        data-success={success ? 'true' : undefined}
      />
      {helpText ? <div id={`${id}--help`} className="gj-field__help">{helpText}</div> : null}
      <FieldStatus id={id} error={error} success={success} />
    </div>
  );
}

export interface SelectFieldProps
  extends FieldMeta,
    Omit<SelectHTMLAttributes<HTMLSelectElement>, 'id' | 'aria-invalid'> {
  options: Array<{ value: string; label: string; disabled?: boolean }>;
}

export function SelectField({ id, label, helpText, error, success, options, className, ...props }: SelectFieldProps) {
  return (
    <div className="gj-field">
      <label className="gj-field__label" htmlFor={id}>{label}</label>
      <select
        {...props}
        id={id}
        className={['gj-select', className].filter(Boolean).join(' ')}
        aria-invalid={error ? true : undefined}
        aria-describedby={describedBy([
          helpText ? `${id}--help` : undefined,
          error ? `${id}--error` : undefined,
          success ? `${id}--status` : undefined,
        ])}
        data-success={success ? 'true' : undefined}
      >
        {options.map((option) => (
          <option key={option.value} value={option.value} disabled={option.disabled}>{option.label}</option>
        ))}
      </select>
      {helpText ? <div id={`${id}--help`} className="gj-field__help">{helpText}</div> : null}
      <FieldStatus id={id} error={error} success={success} />
    </div>
  );
}

export interface CheckboxProps extends Omit<InputHTMLAttributes<HTMLInputElement>, 'type'> {
  label: string;
}

export function Checkbox({ label, id, ...props }: CheckboxProps) {
  const fallbackId = useId();
  const controlId = id ?? `gj-checkbox-${fallbackId}`;
  return (
    <label className="gj-checkbox" htmlFor={controlId}>
      <input {...props} id={controlId} className="gj-checkbox__control" type="checkbox" />
      <span>{label}</span>
    </label>
  );
}

export interface SwitchProps {
  id: string;
  label: string;
  checked: boolean;
  disabled?: boolean;
  onChange: (checked: boolean) => void;
}

export function Switch({ id, label, checked, disabled, onChange }: SwitchProps) {
  return (
    <label className="gj-switch" htmlFor={id}>
      <input
        id={id}
        className="gj-switch__control"
        type="checkbox"
        role="switch"
        checked={checked}
        disabled={disabled}
        onChange={(event) => onChange(event.currentTarget.checked)}
      />
      <span>{label}</span>
      <span className="gj-switch__state">{checked ? 'On' : 'Off'}</span>
    </label>
  );
}

export interface CardProps extends HTMLAttributes<HTMLElement> {
  selected?: boolean;
  elevated?: boolean;
  as?: 'article' | 'section' | 'div';
}

export function Card({ selected = false, elevated = false, as = 'div', className, ...props }: CardProps) {
  const Element = as;
  return (
    <Element
      {...props}
      className={['gj-card', className].filter(Boolean).join(' ')}
      data-selected={selected ? 'true' : undefined}
      data-elevated={elevated ? 'true' : undefined}
    />
  );
}

function FeedbackIcon({ variant }: { variant: FeedbackVariant }) {
  if (variant === 'success') return <CheckCircle2 aria-hidden="true" />;
  if (variant === 'warning') return <AlertTriangle aria-hidden="true" />;
  if (variant === 'danger') return <CircleAlert aria-hidden="true" />;
  return <Info aria-hidden="true" />;
}

export function InlineMessage({ variant, children }: { variant: FeedbackVariant; children: ReactNode }) {
  return (
    <div className="gj-message" data-variant={variant} role={variant === 'danger' ? 'alert' : 'status'}>
      <FeedbackIcon variant={variant} />
      <div>{children}</div>
    </div>
  );
}

export interface TabsProps {
  tabs: Array<{ id: string; label: string; disabled?: boolean }>;
  activeId: string;
  onChange: (id: string) => void;
  ariaLabel: string;
}

export function Tabs({ tabs, activeId, onChange, ariaLabel }: TabsProps) {
  function onKeyDown(event: KeyboardEvent<HTMLButtonElement>) {
    if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) return;
    const list = Array.from(event.currentTarget.parentElement?.querySelectorAll<HTMLButtonElement>('[role="tab"]') ?? [])
      .filter((item) => !item.disabled);
    if (!list.length) return;
    const current = list.indexOf(event.currentTarget);
    let next = current;
    if (event.key === 'ArrowRight') next = (current + 1) % list.length;
    if (event.key === 'ArrowLeft') next = (current - 1 + list.length) % list.length;
    if (event.key === 'Home') next = 0;
    if (event.key === 'End') next = list.length - 1;
    event.preventDefault();
    list[next]?.focus();
  }

  return (
    <div className="gj-tabs" role="tablist" aria-label={ariaLabel}>
      {tabs.map((tab) => (
        <button
          key={tab.id}
          id={`${tab.id}--tab`}
          className="gj-tab"
          role="tab"
          type="button"
          aria-selected={tab.id === activeId}
          aria-controls={`${tab.id}--panel`}
          tabIndex={tab.id === activeId ? 0 : -1}
          disabled={tab.disabled}
          onKeyDown={onKeyDown}
          onClick={() => onChange(tab.id)}
        >
          {tab.label}
        </button>
      ))}
    </div>
  );
}

export interface BreadcrumbProps {
  items: Array<{ label: string; href?: string }>;
  ariaLabel?: string;
}

export function Breadcrumb({ items, ariaLabel = 'Breadcrumb' }: BreadcrumbProps) {
  return (
    <nav className="gj-breadcrumb" aria-label={ariaLabel}>
      <ol>
        {items.map((item, index) => {
          const current = index === items.length - 1;
          return (
            <li key={`${item.label}-${index}`}>
              {item.href && !current ? <InlineLink href={item.href}>{item.label}</InlineLink> : <span aria-current={current ? 'page' : undefined}>{item.label}</span>}
            </li>
          );
        })}
      </ol>
    </nav>
  );
}

export interface NavigationProps {
  label: string;
  items: Array<{ label: string; href: string; current?: boolean; unavailableReason?: string }>;
}

export function Navigation({ label, items }: NavigationProps) {
  return (
    <nav className="gj-navigation" aria-label={label}>
      <ul>
        {items.map((item) => (
          <li key={item.href}>
            {item.unavailableReason ? (
              <span title={item.unavailableReason}>{item.label} — {item.unavailableReason}</span>
            ) : (
              <a className="gj-navigation__link" href={item.href} aria-current={item.current ? 'page' : undefined}>{item.label}</a>
            )}
          </li>
        ))}
      </ul>
    </nav>
  );
}

export interface PaginationProps {
  current: number;
  total: number;
  onPageChange: (page: number) => void;
  ariaLabel?: string;
}

export function Pagination({ current, total, onPageChange, ariaLabel = 'Pagination' }: PaginationProps) {
  const pages = Array.from({ length: Math.max(total, 0) }, (_, index) => index + 1);
  return (
    <nav aria-label={ariaLabel}>
      <ul className="gj-pagination">
        <li><button className="gj-pagination__button" type="button" aria-label="Previous page" disabled={current <= 1} onClick={() => onPageChange(current - 1)}><ChevronLeft aria-hidden="true" /></button></li>
        {pages.map((page) => (
          <li key={page}><button className="gj-pagination__button" type="button" aria-current={page === current ? 'page' : undefined} onClick={() => onPageChange(page)}>{page}</button></li>
        ))}
        <li><button className="gj-pagination__button" type="button" aria-label="Next page" disabled={current >= total} onClick={() => onPageChange(current + 1)}><ChevronRight aria-hidden="true" /></button></li>
      </ul>
    </nav>
  );
}

export function DataTable({ caption, children }: { caption: string; children: ReactNode }) {
  return (
    <div className="gj-table-region" tabIndex={0} aria-label={`${caption}. Scrollable data region when needed.`}>
      <table className="gj-table">
        <caption>{caption}</caption>
        {children}
      </table>
    </div>
  );
}

export interface EmptyStateProps {
  title: string;
  reason: string;
  action?: ReactNode;
  helpHref?: string;
  helpLabel?: string;
}

export function EmptyState({ title, reason, action, helpHref, helpLabel = 'Learn more' }: EmptyStateProps) {
  return (
    <section className="gj-empty" aria-labelledby={`${title.replace(/\s+/g, '-').toLowerCase()}--title`}>
      <Search className="gj-empty__icon" aria-hidden="true" />
      <h2 id={`${title.replace(/\s+/g, '-').toLowerCase()}--title`}>{title}</h2>
      <p>{reason}</p>
      {action}
      {helpHref ? <InlineLink href={helpHref}>{helpLabel}</InlineLink> : null}
    </section>
  );
}

export interface DialogProps {
  open: boolean;
  title: string;
  description?: string;
  children: ReactNode;
  actions?: ReactNode;
  onClose: () => void;
}

export function Dialog({ open, title, description, children, actions, onClose }: DialogProps) {
  const dialogRef = useRef<HTMLDialogElement>(null);
  const titleId = useId();
  const descriptionId = useId();

  useEffect(() => {
    const dialog = dialogRef.current;
    if (!dialog) return;
    if (open && !dialog.open) dialog.showModal();
    if (!open && dialog.open) dialog.close();
  }, [open]);

  return (
    <dialog
      ref={dialogRef}
      className="gj-dialog"
      aria-labelledby={titleId}
      aria-describedby={description ? descriptionId : undefined}
      onCancel={(event) => { event.preventDefault(); onClose(); }}
      onClose={onClose}
    >
      <div className="gj-dialog__body">
        <div className="gj-dialog__header">
          <div>
            <h2 id={titleId}>{title}</h2>
            {description ? <p id={descriptionId}>{description}</p> : null}
          </div>
          <button className="gj-dialog__close" type="button" aria-label={`Close ${title}`} onClick={onClose}><X aria-hidden="true" /></button>
        </div>
        {children}
        {actions ? <div className="gj-dialog__actions">{actions}</div> : null}
      </div>
    </dialog>
  );
}

export function Tooltip({ label, content }: { label: string; content: string }) {
  const id = useId();
  return (
    <span className="gj-tooltip">
      <button className="gj-tooltip__trigger" type="button" aria-describedby={id}>{label}</button>
      <span id={id} className="gj-tooltip__bubble" role="tooltip">{content}</span>
    </span>
  );
}

export interface ComboboxOption {
  id: string;
  label: string;
  disabled?: boolean;
}

export interface ComboboxProps {
  id: string;
  label: string;
  options: ComboboxOption[];
  query: string;
  onQueryChange: (value: string) => void;
  onSelect: (option: ComboboxOption) => void;
  loading?: boolean;
  error?: string;
}

export function Combobox({ id, label, options, query, onQueryChange, onSelect, loading = false, error }: ComboboxProps) {
  const [open, setOpen] = useState(false);
  const [activeIndex, setActiveIndex] = useState(0);
  const enabled = options.filter((option) => !option.disabled);
  const active = enabled[activeIndex];
  const listId = `${id}--listbox`;

  function choose(option: ComboboxOption | undefined) {
    if (!option || option.disabled) return;
    onSelect(option);
    onQueryChange(option.label);
    setOpen(false);
  }

  function onKeyDown(event: KeyboardEvent<HTMLInputElement>) {
    if (event.key === 'ArrowDown') {
      event.preventDefault(); setOpen(true); setActiveIndex((value) => enabled.length ? (value + 1) % enabled.length : 0);
    } else if (event.key === 'ArrowUp') {
      event.preventDefault(); setOpen(true); setActiveIndex((value) => enabled.length ? (value - 1 + enabled.length) % enabled.length : 0);
    } else if (event.key === 'Home') {
      event.preventDefault(); setOpen(true); setActiveIndex(0);
    } else if (event.key === 'End') {
      event.preventDefault(); setOpen(true); setActiveIndex(Math.max(enabled.length - 1, 0));
    } else if (event.key === 'Enter' && open) {
      event.preventDefault(); choose(active);
    } else if (event.key === 'Escape') {
      event.preventDefault(); setOpen(false);
    }
  }

  return (
    <div className="gj-field">
      <label className="gj-field__label" htmlFor={id}>{label}</label>
      <div className="gj-combobox">
        <input
          id={id}
          className="gj-combobox__input"
          role="combobox"
          aria-autocomplete="list"
          aria-expanded={open}
          aria-controls={listId}
          aria-activedescendant={open && active ? `${id}--option-${active.id}` : undefined}
          aria-invalid={error ? true : undefined}
          value={query}
          onFocus={() => setOpen(true)}
          onChange={(event: ChangeEvent<HTMLInputElement>) => { onQueryChange(event.currentTarget.value); setOpen(true); setActiveIndex(0); }}
          onKeyDown={onKeyDown}
        />
        {open ? (
          <ul id={listId} className="gj-combobox__list" role="listbox">
            {options.map((option) => {
              const selected = active?.id === option.id;
              return (
                <li
                  key={option.id}
                  id={`${id}--option-${option.id}`}
                  className="gj-combobox__option"
                  role="option"
                  aria-selected={selected}
                  aria-disabled={option.disabled || undefined}
                  onMouseDown={(event) => event.preventDefault()}
                  onClick={() => choose(option)}
                >
                  {selected ? <CheckCircle2 aria-hidden="true" /> : null}
                  {option.label}
                </li>
              );
            })}
          </ul>
        ) : null}
      </div>
      <div className="gj-field__help" aria-live="polite">{loading ? 'Loading results' : `${enabled.length} results`}</div>
      {error ? <FieldStatus id={id} error={error} /> : null}
    </div>
  );
}

export interface ProgressRegionProps {
  title: string;
  value?: number;
  max?: number;
  status: string;
  action?: ReactNode;
}

export function ProgressRegion({ title, value, max = 100, status, action }: ProgressRegionProps) {
  return (
    <section className="gj-progress-region" aria-busy={value === undefined} aria-labelledby={`${title}--progress-title`}>
      <h2 id={`${title}--progress-title`}>{title}</h2>
      {value === undefined ? <LoaderCircle aria-hidden="true" /> : <progress value={value} max={max}>{value}</progress>}
      <div aria-live="polite">{status}</div>
      {action}
    </section>
  );
}

export function SelectionBar({ count, children }: { count: number; children: ReactNode }) {
  return (
    <section className="gj-selection-bar" aria-label={`${count} selected`}>
      <strong>{count} selected</strong>
      <div>{children}</div>
    </section>
  );
}

export interface ChartFrameProps {
  title: string;
  unit: string;
  timeRange: string;
  children: ReactNode;
  dataTable: ReactNode;
}

export function ChartFrame({ title, unit, timeRange, children, dataTable }: ChartFrameProps) {
  const titleId = useId();
  return (
    <figure className="gj-chart-frame" aria-labelledby={titleId}>
      <figcaption id={titleId}>{title}</figcaption>
      <div className="gj-chart-frame__meta"><span>{unit}</span><span>{timeRange}</span></div>
      <div>{children}</div>
      <details><summary>View data table</summary>{dataTable}</details>
    </figure>
  );
}

export function ToastRegion({ messages }: { messages: Array<{ id: string; variant: FeedbackVariant; content: ReactNode }> }) {
  return (
    <div className="gj-toast-region" aria-live="polite" aria-relevant="additions text">
      {messages.map((message) => <InlineMessage key={message.id} variant={message.variant}>{message.content}</InlineMessage>)}
    </div>
  );
}

export interface DestructiveConfirmationProps {
  open: boolean;
  title: string;
  description: string;
  confirmLabel: string;
  onConfirm: () => void;
  onClose: () => void;
  loading?: boolean;
}

export function DestructiveConfirmation({ open, title, description, confirmLabel, onConfirm, onClose, loading = false }: DestructiveConfirmationProps) {
  return (
    <Dialog
      open={open}
      title={title}
      description={description}
      onClose={onClose}
      actions={(
        <>
          <Button onClick={onClose}>Cancel</Button>
          <Button variant="destructive" loading={loading} onClick={onConfirm}>{confirmLabel}</Button>
        </>
      )}
    >
      <InlineMessage variant="warning">This action changes persisted state. Review the object and consequence before confirming.</InlineMessage>
    </Dialog>
  );
}

export interface CommandPaletteProps {
  open: boolean;
  query: string;
  results: Array<{ id: string; label: string; unavailableReason?: string }>;
  onQueryChange: (value: string) => void;
  onSelect: (id: string) => void;
  onClose: () => void;
}

export function CommandPalette({ open, query, results, onQueryChange, onSelect, onClose }: CommandPaletteProps) {
  return (
    <Dialog open={open} title="Command palette" onClose={onClose}>
      <TextField id="command-palette--query" label="Search commands" value={query} onChange={(event) => onQueryChange(event.currentTarget.value)} autoComplete="off" />
      <nav aria-label="Command results">
        <ul>
          {results.map((result) => (
            <li key={result.id}>
              <Button disabled={Boolean(result.unavailableReason)} onClick={() => onSelect(result.id)}>
                {result.label}{result.unavailableReason ? ` — ${result.unavailableReason}` : ''}
              </Button>
            </li>
          ))}
        </ul>
      </nav>
    </Dialog>
  );
}

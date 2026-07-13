import { useState } from 'react';
import { Ico } from './icons';
import { useT } from './i18n';
import './styles/password.css';

// PasswordField is the suite standard for any password entry — login, forced
// change, camera credentials, settings — so the control looks and behaves the same
// everywhere: a normal input with an in-field reveal (eye) toggle. The toggle is
// tabIndex={-1} so it never sits between the password and the submit button in the
// tab order.
export function PasswordField({ value, onChange, autoComplete = 'off', autoFocus = false, placeholder, disabled = false, error = false }) {
  const t = useT();
  const [show, setShow] = useState(false);
  return (
    <div className={`password-field${error ? ' input-error' : ''}`}>
      <input
        value={value}
        onChange={(event) => onChange(event.target.value)}
        type={show ? 'text' : 'password'}
        autoComplete={autoComplete}
        autoFocus={autoFocus}
        placeholder={placeholder}
        disabled={disabled}
      />
      <button
        type="button"
        className="password-reveal"
        onClick={() => setShow((s) => !s)}
        aria-label={show ? t('common.hidePassword') : t('common.showPassword')}
        aria-pressed={show}
        tabIndex={-1}
        disabled={disabled}
      >
        <Ico n={show ? 'eye-off' : 'eye'} sz={16} />
      </button>
    </div>
  );
}

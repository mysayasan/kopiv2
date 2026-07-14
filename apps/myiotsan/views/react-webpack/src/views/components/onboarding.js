import { useState } from 'react';
import { Ico, useT } from '@shared';
import { Modal } from './ui';

// FirstRunWizard is the short, honest path a brand-new hub offers an admin on their first
// sign-in — and only then: it appears when the estate is genuinely empty and disappears for
// good once it has been finished or skipped.
//
// It explains and it hands off. It does NOT re-implement the enrollment window inside a
// dialog: opening a door that lets unknown things onto the broker is the one act in the app
// that deserves its own page, its own security explanation, and its own show-once key
// dialog. A wizard step that quietly does the same thing in a smaller box would be teaching
// an admin to treat it as a formality. So the last step walks them to Discovery instead.
//
// There are three steps and all three are real. None of them fakes progress, none of them
// pretends to configure something it has not, and every one of them can be skipped.
const STEPS = ['what', 'enrol', 'ready'];

export function FirstRunWizard({ onGoDiscovery, onDismiss }) {
  const t = useT();
  const [step, setStep] = useState(0);
  const id = STEPS[step];
  const last = step === STEPS.length - 1;

  return (
    <Modal title={t('wiz.title')} onClose={onDismiss} wide>
      <p className="iot-wiz-step">{t('wiz.step', { n: step + 1, total: STEPS.length })}</p>

      <div className="iot-wiz-body">
        {id === 'what' ? (
          <>
            <h3><span className="btn-icon"><Ico n="monitor" sz={15} /> {t('wiz.what.title')}</span></h3>
            <p>{t('wiz.what.body')}</p>
            <p className="settings-hint">{t('wiz.what.empty')}</p>
          </>
        ) : null}

        {id === 'enrol' ? (
          <>
            <h3><span className="btn-icon"><Ico n="key" sz={15} /> {t('wiz.enrol.title')}</span></h3>
            <p>{t('wiz.enrol.body')}</p>
            <p className="iot-wiz-quarantine">
              <Ico n="lock" sz={14} /> <span>{t('wiz.enrol.quarantine')}</span>
            </p>
          </>
        ) : null}

        {id === 'ready' ? (
          <>
            <h3><span className="btn-icon"><Ico n="check-ok" sz={15} /> {t('wiz.ready.title')}</span></h3>
            <p>{t('wiz.ready.body')}</p>
            <ol className="iot-wiz-steps">
              <li>{t('wiz.ready.s1')}</li>
              <li>{t('wiz.ready.s2')}</li>
              <li>{t('wiz.ready.s3')}</li>
            </ol>
          </>
        ) : null}
      </div>

      <div className="modal-actions iot-wiz-actions">
        <button type="button" className="quiet" onClick={onDismiss}>{t('wiz.skip')}</button>
        {step > 0 ? (
          <button type="button" className="quiet" onClick={() => setStep((s) => s - 1)}>
            <span className="btn-icon"><Ico n="arr-left" sz={14} /> {t('wiz.back')}</span>
          </button>
        ) : null}
        {last ? (
          <button type="button" onClick={onGoDiscovery}>
            <span className="btn-icon"><Ico n="search" sz={14} /> {t('wiz.goDiscovery')}</span>
          </button>
        ) : (
          <button type="button" onClick={() => setStep((s) => s + 1)}>
            <span className="btn-icon">{t('wiz.next')} <Ico n="arr-right" sz={14} /></span>
          </button>
        )}
      </div>
    </Modal>
  );
}

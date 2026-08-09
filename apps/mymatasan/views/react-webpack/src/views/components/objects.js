import { useState } from 'react';
import { Tabs } from '@shared/Tabs';
import { useT } from '@shared/i18n';
import { HelpButton } from '@shared/Manual';
import { CameraObjectSearchPanel } from './recording';
import { ObjectClassesPanel } from './vision';

// ObjectsPage merges the former "Object Search" and "Object Classes" pages into one
// surface under a single nav entry, split by a shared <Tabs> bar:
//   - Search  → find objects the AI has already detected (across all cameras), with
//               click-to-footage (CameraObjectSearchPanel).
//   - Classes → curate the detection-class registry that shapes what gets detected
//               (ObjectClassesPanel).
// Search sits first because it's the everyday task; Classes is the configuration behind it.
export function ObjectsPage({ authHeader, busy, isAdmin, classes, labelCatalog, activeModelClasses, onSaveClass, onDeleteClass }) {
  const t = useT();
  const [tab, setTab] = useState('search');
  return (
    <section className="workspace objects-page">
      <div className="objects-help">
        <HelpButton slug="object-search" anchor="enabling" />
      </div>
      <Tabs
        ariaLabel={t('obj.tabsAria')}
        active={tab}
        onChange={setTab}
        tabs={[
          { id: 'search', label: t('obj.tabSearch'), icon: 'eye' },
          { id: 'classes', label: t('obj.tabClasses'), icon: 'list' },
        ]}
      />
      {tab === 'search' ? (
        <CameraObjectSearchPanel authHeader={authHeader} busy={busy} canManage={isAdmin} />
      ) : (
        <ObjectClassesPanel
          classes={classes}
          labelCatalog={labelCatalog}
          activeModelClasses={activeModelClasses}
          busy={busy}
          onSaveClass={onSaveClass}
          onDeleteClass={onDeleteClass}
        />
      )}
    </section>
  );
}

import { Modal } from './ui'

const GROUPS: { title: string; keys: [string[], string][] }[] = [
  { title: 'Go to', keys: [[['h'], 'Home'], [['r'], 'Recipes'], [['w'], 'Meal plan (week)'], [['s'], 'Shopping'], [['p'], 'Pantry'], [[','], 'Settings'], [['/'], 'Search & commands'], [['⌘', 'K'], 'Search & commands']] },
  { title: 'New', keys: [[['n'], 'Recipe'], [['i'], 'Import a recipe from a link'], [['m'], 'Plan a meal'], [['a'], 'Add to the shopping list'], [['e'], 'Pantry item'], [['b'], 'Scan a barcode']] },
  { title: 'Window', keys: [[['['], 'Show or hide the sidebar'], [['esc'], 'Close a dialog or menu']] },
  { title: 'Help', keys: [[['?'], 'This sheet']] },
]

export function ShortcutsSheet({ onClose }: { onClose: () => void }) {
  return (
    <Modal title="Keyboard shortcuts" onClose={onClose} size="lg">
      <div className="shortcuts">
        {GROUPS.map(g => (
          <section key={g.title}>
            <h4 className="sc-h">{g.title}</h4>
            {g.keys.map(([keys, label]) => (
              <div key={label + keys.join('')} className="sc-row"><span>{label}</span><span className="sc-keys">{keys.map(k => <kbd key={k}>{k}</kbd>)}</span></div>
            ))}
          </section>
        ))}
      </div>
      <p className="muted small mt">“Add to the shopping list” goes to the list you have open, or your first list.</p>
    </Modal>
  )
}

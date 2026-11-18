// Recommended extensions on the Extensions page: file hosts from the official
// Gopeed store. None is installed or enabled by itself; an administrator
// installs one through the usual catalog flow. Entries match by store identity
// or repository, never by display title; one the store no longer lists is hidden.
// Gofile has two store entries: the one maintained in 2026 (website token fix)
// over the older one with more stars.
export const recommendedExtensions = [
  { key: 'gofile', id: 'Gwkang@gopeed-extension-gofile', repo: 'https://github.com/iGwkang/gopeed-extension-gofile' },
  { key: 'mediafire', id: 'Locon213@gopeed-extension-mediafire', repo: 'https://github.com/Locon213/gopeed-extension-mediafire' },
  { key: 'pixeldrain', id: 'Locon213@gopeed-extension-pixeldrain', repo: 'https://github.com/Locon213/gopeed-extension-pixeldrain' },
  { key: 'debrid', id: 'silasmariusz@debrid', repo: 'https://github.com/silasmariusz/gopeed-ext-debrid' },
] as const

// DSM's page policy (img-src) blocks store icons hosted elsewhere, such as
// raw.githubusercontent.com, so only an inline or same-origin icon is shown;
// any other card gets the plug glyph without a blocked request.
export function cardIcon(icon: unknown) {
  return typeof icon === 'string' && (/^data:image\/(png|jpeg|gif|webp);base64,/i.test(icon) || /^\/(?!\/)/.test(icon)) ? icon : undefined
}

const repoKey = (url: unknown) =>
  typeof url === 'string' ? url.trim().toLowerCase().replace(/#.*$/, '').replace(/\.git$/, '').replace(/\/+$/, '') : ''

export function recommendedCards(catalog: any[], installed: any[]) {
  return recommendedExtensions.flatMap((rec) => {
    const repo = repoKey(rec.repo)
    const entry = catalog.find((x) => x.id === rec.id || (repo && repoKey(x.repoUrl || x.url) === repo))
    if (!entry) return []
    const done = installed.some((x) => x.identity === rec.id || x.identity === entry.id || repoKey(x.repository?.url) === repo)
    return [{ key: rec.key, entry, installed: done }]
  })
}

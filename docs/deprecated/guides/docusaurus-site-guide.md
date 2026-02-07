# Docusaurus Documentation Site Guide

A practical guide for working on the Sidecar documentation site (`website/`). This covers local development, editing the main page, writing docs, and deployment.

## Quick Start

Requires Node.js >= 20.0

```bash
cd website
npm install    # First time only
npm start      # Dev server at http://localhost:3000
```

The dev server hot-reloads on file changes.

> **Note**: This guide uses `npm` commands. The `website/README.md` still references `yarn` (legacy) but both work.

## Project Structure

```
website/
├── docs/                    # Markdown documentation pages
│   ├── intro.md            # Getting Started (sidebar_position: 1)
│   ├── td.md               # TD task manager docs (sidebar_position: 2)
│   └── git-plugin.md       # Git plugin docs (sidebar_position: 3)
├── blog/                    # Blog posts (date-prefixed markdown)
│   ├── authors.yml         # Blog author definitions
│   └── tags.yml            # Blog tag definitions
├── src/
│   ├── pages/              # Custom React pages (non-docs)
│   │   ├── index.js        # Front page (/)
│   │   └── index.module.css
│   ├── components/         # Reusable React components
│   └── css/
│       └── custom.css      # Global style overrides
├── static/                  # Static assets (copied as-is to build)
│   ├── img/                # Images
│   └── .nojekyll           # Prevents GitHub Pages Jekyll processing
├── docusaurus.config.js    # Main site configuration
├── sidebars.js             # Docs sidebar structure
└── package.json            # Dependencies
```

## Editing the Front Page

The front page is at `website/src/pages/index.js`. It's a React component using Docusaurus's Layout and theming.

**Key elements**:
- `HomepageHeader`: Hero section with title/tagline from config
- `Home`: Main layout wrapper

To modify:
```jsx
// src/pages/index.js
export default function Home() {
  return (
    <Layout title="Home" description="...">
      <HomepageHeader />
      <main className="container">
        {/* Add content here */}
      </main>
    </Layout>
  );
}
```

**Styling**: Use `index.module.css` for page-specific styles or `src/css/custom.css` for global overrides.

**Docusaurus Reference**: [Creating Pages](https://docusaurus.io/docs/creating-pages)

## Writing Documentation

> **For AI agents**: Use the `dev-docs` skill (`~/.claude/skills/dev-docs/SKILL.md`) when writing documentation. It provides patterns for user-first, scannable docs with proper code examples.

Docs live in `website/docs/` as Markdown files with YAML frontmatter.

### Documentation Principles

Follow these patterns from the dev-docs skill:

- **User-first**: Answer "what can I do?" before "how does it work?"
- **Scannable**: Use headers, code blocks, tables for keyboard shortcuts
- **Progressive disclosure**: Quick overview → detailed usage → full reference
- **Working examples**: Every feature needs runnable code, not `...` placeholders

### Plugin Documentation Pattern

For Sidecar plugins, use this structure:

```markdown
# Plugin Name

One-line description of what it does.

![Screenshot](../../docs/screenshots/plugin-name.png)

## Overview
Brief explanation of the UI layout and core purpose.

## Feature Section
Description with keyboard shortcut table:

| Key | Action |
|-----|--------|
| `s` | Stage file |
| `d` | View diff |

## Navigation
How to move around within the plugin.

## Command Reference
Complete list of all shortcuts by context.
```

### Creating a New Doc

```markdown
---
sidebar_position: 2
title: My New Page
---

# My New Page

Content goes here. Supports **Markdown** and MDX.
```

**Frontmatter options**:
- `sidebar_position`: Order in sidebar (lower = higher)
- `sidebar_label`: Override sidebar text (defaults to title)
- `title`: Page title
- `description`: Meta description for SEO
- `slug`: Custom URL path

### Sidebar Configuration

The sidebar auto-generates from the `docs/` folder structure. To customize, edit `sidebars.js`:

```javascript
// sidebars.js
const sidebars = {
  tutorialSidebar: [
    'intro',
    {
      type: 'category',
      label: 'Guides',
      items: ['guides/installation', 'guides/usage'],
    },
  ],
};
```

**Docusaurus Reference**: [Docs Introduction](https://docusaurus.io/docs/docs-introduction)

### Organizing Docs in Folders

Create folders to group related docs:

```
docs/
├── intro.md
├── guides/
│   ├── _category_.json    # Folder metadata
│   ├── installation.md
│   └── configuration.md
└── plugins/
    ├── _category_.json
    └── git-status.md
```

The `_category_.json` file controls folder appearance:

```json
{
  "label": "Guides",
  "position": 2,
  "collapsible": true,
  "collapsed": false
}
```

## Site Configuration

Main config is in `docusaurus.config.js`. Key sections:

### Basic Info
```javascript
const config = {
  title: 'Sidecar',
  tagline: 'Terminal UI for monitoring AI coding agent sessions',
  url: 'https://marcus.github.io',
  baseUrl: '/sidecar/',
  organizationName: 'marcus',
  projectName: 'sidecar',
};
```

### Navbar
```javascript
themeConfig: {
  navbar: {
    title: 'Sidecar',
    items: [
      { type: 'docSidebar', sidebarId: 'tutorialSidebar', label: 'Docs' },
      { to: '/blog', label: 'Blog' },
      { href: 'https://github.com/marcus/sidecar', label: 'GitHub' },
    ],
  },
}
```

### Custom Navbar Items

The navbar includes a custom theme switcher component. Custom navbar items are defined in `src/theme/NavbarItem/index.js` wrapping components from `src/components/`.

### Theme Configuration

Dark mode is configured with `disableSwitch: true` in the theme config, meaning users cannot toggle between light and dark modes.

### Footer
```javascript
footer: {
  style: 'dark',
  links: [
    { title: 'Docs', items: [{ label: 'Getting Started', to: '/docs/intro' }] },
  ],
  copyright: `Copyright © ${new Date().getFullYear()} Sidecar.`,
}
```

### Future Compatibility

The config includes `future: { v4: true }` for Docusaurus v4 compatibility, enabling upcoming features and ensuring a smoother upgrade path.

**Docusaurus Reference**: [Configuration](https://docusaurus.io/docs/configuration)

## Adding Images

### Screenshot Organization

**Website documentation screenshots** (for Docusaurus pages in `website/docs/`):
- Store in: `docs/screenshots/` (project root)
- Reference with: `![Alt text](../../docs/screenshots/filename.png)`
- These are the official screenshots for the documentation site

**README and other markdown screenshots** (for GitHub, repo docs, etc.):
- Store in: `docs/screenshots/` (project root)
- Reference with: `![Alt text](docs/screenshots/filename.png)` (from repo root)
- Same location, different relative path based on context

**General website images** (logos, icons, graphics):
- Store in: `website/static/img/`
- Reference with: `![Alt text](/img/filename.png)`

### For AI Agents

When capturing screenshots:
- If the prompt mentions "website docs" or "Docusaurus": save to `docs/screenshots/`
- If the prompt mentions "README" or "repo documentation": save to `docs/screenshots/`
- The same screenshots in `docs/screenshots/` serve both website and repo docs

### Usage Examples

In website docs (`website/docs/*.md`):
```markdown
![Plugin Screenshot](../../docs/screenshots/sidecar-git.png)
```

In JSX components:
```jsx
import screenshot from '@site/static/img/logo.png';
<img src={screenshot} alt="Logo" />
```

## Blog Posts

Blog posts are date-prefixed Markdown files in `blog/`:

```
blog/
├── 2026-01-15-welcome.md
└── 2026-02-01-release-notes/
    ├── index.md
    └── screenshot.png
```

### Blog Post Frontmatter

```markdown
---
slug: my-post
title: Post Title
authors: [default]
tags: [announcement, release]
---

Preview text shown in list.

<!-- truncate -->

Full content below the fold.
```

**Docusaurus Reference**: [Blog](https://docusaurus.io/docs/blog)

## Building and Deploying

### Local Build
```bash
cd website
npm run build      # Outputs to website/build/
npm run serve      # Preview built site locally
```

### Deployment

The site deploys automatically via GitHub Actions when changes to `website/` are merged to `main`.

**Workflow files**:
- `.github/workflows/deploy-docs.yml` - Deploys to GitHub Pages
- `.github/workflows/test-docs.yml` - Validates PR builds

**Live site**: https://marcus.github.io/sidecar

**First-time setup**: Enable GitHub Pages in repo Settings → Pages → Source: GitHub Actions

## Style Guidelines

### No Emoji Policy

**Never use emoji** in the site content, components, or documentation. This includes:
- React components (use Lucide icons instead)
- Markdown documentation
- Blog posts
- Code examples
- Comments

Emoji render inconsistently across platforms and don't match the terminal aesthetic. Always use Lucide icon font for visual indicators.

### Icons (Lucide)

The site uses [Lucide](https://lucide.dev) icon font (imported via CDN in `docusaurus.config.js`).

**Usage in JSX:**
```jsx
<i className="icon-copy" />
<i className="icon-check" />
<i className="icon-terminal" />
```

**Common icons for this project:**
- `icon-eye` - monitoring, viewing
- `icon-terminal` - terminal, CLI
- `icon-rocket` - launch, speed
- `icon-check` - success, done
- `icon-copy` - clipboard copy
- `icon-external-link` - external links
- `icon-git-branch` - git operations
- `icon-zap` - fast, instant
- `icon-keyboard` - keyboard shortcuts
- `icon-layers` - multiple items
- `icon-code` - code, programming

**Browse all icons:** https://lucide.dev/icons

### Terminal Aesthetic

Maintain the TUI/terminal visual style:
- Monospace fonts (`JetBrains Mono`, `Google Sans Code`)
- Dark backgrounds with muted colors
- Bright accents for highlights (green, blue, pink, yellow from the Monokai palette)
- Clean 1px borders
- Subtle gradients and glows

## Common Tasks

### Add a new docs section
1. Create folder: `website/docs/plugins/`
2. Add `_category_.json` with label and position
3. Add Markdown files with frontmatter

### Change the theme colors
Edit `src/css/custom.css`:
```css
:root {
  --ifm-color-primary: #2e8555;  /* Main brand color */
}
[data-theme='dark'] {
  --ifm-color-primary: #25c2a0;
}
```

### Add a custom component
1. Create in `src/components/MyComponent/index.js`
2. Import in pages or docs:
   ```jsx
   import MyComponent from '@site/src/components/MyComponent';
   ```

### Use MDX in docs
Docs support MDX (Markdown + JSX):
```mdx
import Tabs from '@theme/Tabs';
import TabItem from '@theme/TabItem';

<Tabs>
  <TabItem value="npm">npm install</TabItem>
  <TabItem value="yarn">yarn add</TabItem>
</Tabs>
```

## Troubleshooting

**Build fails with broken links**: The config uses `onBrokenLinks: 'throw'` to fail builds on broken links. This catches issues early. For local development you can temporarily change to `'warn'` if needed.

**Styles not updating**: Clear cache: `npm run clear && npm start`

**GitHub Pages 404**: Verify `baseUrl` matches repo name (`/sidecar/`).

## Resources

- [Docusaurus Documentation](https://docusaurus.io/docs)
- [Markdown Features](https://docusaurus.io/docs/markdown-features)
- [Styling and Layout](https://docusaurus.io/docs/styling-layout)
- [Deployment to GitHub Pages](https://docusaurus.io/docs/deployment#deploying-to-github-pages)

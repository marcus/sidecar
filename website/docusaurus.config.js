// @ts-check
// `@type` JSDoc annotations allow editor autocompletion and type checking
// (when paired with `@ts-check`).
// There are various equivalent ways to declare your Docusaurus config.
// See: https://docusaurus.io/docs/api/docusaurus-config

import {execSync} from 'child_process';
import {themes as prismThemes} from 'prism-react-renderer';

// Compute commit count and version from git at build time
let commitCount = 2374;
let currentVersion = '1.5';
try {
  const countStr = execSync('git rev-list --count HEAD', {encoding: 'utf8'}).trim();
  const parsed = parseInt(countStr, 10);
  if (!isNaN(parsed) && parsed > 0) {
    commitCount = parsed;
  }
} catch {
  // Fallback if git is not available
}

try {
  const verStr = execSync('git describe --tags --abbrev=0', {encoding: 'utf8'}).trim();
  if (verStr) {
    currentVersion = verStr.replace(/^v/, '');
  }
} catch {
  // Fallback
}

// This runs in Node.js - Don't use client-side code here (browser APIs, JSX...)

/** @type {import('@docusaurus/types').Config} */
const config = {
  title: 'Sidecar',
  tagline: 'Everything you need to develop, across every project, in a single terminal.',
  favicon: 'img/favicon.ico',

  // Future flags, see https://docusaurus.io/docs/api/docusaurus-config#future
  future: {
    v4: true, // Improve compatibility with the upcoming Docusaurus v4
  },

  // Set the production url of your site here
  url: 'https://sidecar.haplab.com',
  // Set the /<baseUrl>/ pathname under which your site is served
  // For GitHub pages deployment, it is often '/<projectName>/'
  baseUrl: '/',

  // GitHub pages deployment config.
  organizationName: 'marcus',
  projectName: 'sidecar',
  trailingSlash: false,

  customFields: {
    githubUrl: 'https://github.com/marcus/sidecar',
    commitCount,
    currentVersion,
  },

  onBrokenLinks: 'throw',

  // Even if you don't use internationalization, you can use this field to set
  // useful metadata like html lang. For example, if your site is Chinese, you
  // may want to replace "en" with "zh-Hans".
  i18n: {
    defaultLocale: 'en',
    locales: ['en'],
  },

  headTags: [
    {
      tagName: 'link',
      attributes: {
        rel: 'preconnect',
        href: 'https://fonts.googleapis.com',
      },
    },
    {
      tagName: 'link',
      attributes: {
        rel: 'preconnect',
        href: 'https://fonts.gstatic.com',
        crossorigin: 'anonymous',
      },
    },
  ],

  stylesheets: [
    {
      href: 'https://fonts.googleapis.com/css2?family=Archivo:wght@400..800&family=JetBrains+Mono:wght@300..600&family=Caveat:wght@400..700&family=Kalam:wght@300;400;700&family=Architects+Daughter&display=swap',
      type: 'text/css',
    },
  ],

  presets: [
    [
      'classic',
      /** @type {import('@docusaurus/preset-classic').Options} */
      ({
        docs: {
          sidebarPath: './sidebars.js',
          editUrl: 'https://github.com/marcus/sidecar/tree/main/website/',
        },
        blog: {
          showReadingTime: true,
          feedOptions: {
            type: ['rss', 'atom'],
            xslt: true,
          },
          editUrl: 'https://github.com/marcus/sidecar/tree/main/website/',
          onInlineTags: 'warn',
          onInlineAuthors: 'warn',
          onUntruncatedBlogPosts: 'warn',
        },
        theme: {
          customCss: './src/css/custom.css',
        },
      }),
    ],
  ],

  plugins: [
    [
      '@docusaurus/plugin-client-redirects',
      {
        // Kept URLs. A page that moved keeps answering at the address people
        // and search engines already have.
        redirects: [
          {from: '/docs/terminal-resources', to: '/docs/plugins'},
        ],
      },
    ],
  ],

  themeConfig:
    /** @type {import('@docusaurus/preset-classic').ThemeConfig} */
    ({
      image: 'img/sidecar-logo.png',
      metadata: [
        {name: 'twitter:card', content: 'summary_large_image'},
        {name: 'twitter:image:alt', content: 'Sidecar - Full development context in a single terminal'},
      ],
      colorMode: {
        defaultMode: 'dark',
        disableSwitch: true,
        respectPrefersColorScheme: false,
      },
      navbar: {
        // The wordmark is type, not an image: `sidecar` in the mono face with
        // the accent-coloured cursor the app draws. See .navbar__title in
        // custom.css.
        title: 'sidecar',
        items: [
          {
            type: 'docSidebar',
            sidebarId: 'tutorialSidebar',
            position: 'left',
            label: 'Docs',
          },
          {
            href: 'https://haplab.com',
            position: 'left',
            label: 'Haplab',
          },
          {
            type: 'custom-themeSwitcher',
            position: 'right',
          },
          {
            href: 'https://github.com/marcus/sidecar',
            label: 'GitHub',
            position: 'right',
          },
          {
            href: 'https://github.com/marcus/sidecar#quick-install',
            label: 'Install',
            position: 'right',
            className: 'navbarInstall',
          },
        ],
      },
      footer: {
        style: 'dark',
        links: [
          {
            title: 'Docs',
            items: [
              {
                label: 'Getting Started',
                to: '/docs/intro',
              },
              {
                label: 'Panes & Layouts',
                to: '/docs/layout-and-panes',
              },
              {
                label: 'Task Management',
                to: '/docs/td',
              },
              {
                label: 'Git Workflow',
                to: '/docs/git-plugin',
              },
              {
                label: 'CLI Reference',
                to: '/docs/cli-reference',
              },
            ],
          },
          {
            title: 'Community',
            items: [
              {
                label: 'GitHub',
                href: 'https://github.com/marcus/sidecar',
              },
              {
                label: 'Issues',
                href: 'https://github.com/marcus/sidecar/issues',
              },
              {
                label: 'Releases',
                href: 'https://github.com/marcus/sidecar/releases',
              },
            ],
          },
          {
            title: 'Sister projects',
            items: [
              {label: 'td', href: 'https://github.com/marcus/td'},
              {label: 'Haplab', href: 'https://haplab.com'},
            ],
          },
        ],
        copyright: `Copyright © ${new Date().getFullYear()} Sidecar.`,
      },
      prism: {
        theme: prismThemes.github,
        darkTheme: prismThemes.dracula,
      },
    }),
};

export default config;

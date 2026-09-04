// @ts-check

/** @type {import('@docusaurus/plugin-content-docs').SidebarsConfig} */
const sidebars = {
  tutorialSidebar: [
    'intro',
    {
      type: 'category',
      label: 'Core Plugins',
      collapsed: false,
      items: [
        'workspaces-plugin',
        'git-plugin',
        'files-plugin',
        'notes-plugin',
        'tasks',
        'td',
        'conversations-plugin',
      ],
    },
    {
      type: 'category',
      label: 'Fleet & Workspaces',
      collapsed: false,
      items: [
        'sessions-and-activity',
        'project-switching',
        'remote-hosts',
      ],
    },
    {
      type: 'category',
      label: 'Windowing & Durability',
      collapsed: false,
      items: [
        'layout-and-panes',
        'session-durability',
        'notifications',
      ],
    },
    {
      type: 'category',
      label: 'Extending Sidecar',
      collapsed: false,
      items: [
        'plugins',
      ],
    },
    {
      type: 'category',
      label: 'Automation & CLI',
      collapsed: false,
      items: [
        'agent-coordination',
        'worktree-setup',
        'cli-reference',
      ],
    },
  ],
};

export default sidebars;

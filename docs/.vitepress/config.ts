import { defineConfig } from "vitepress";
import { tabsMarkdownPlugin } from "vitepress-plugin-tabs";

export default defineConfig({
  title: "A-Novel developer guide",
  description: "Set up a machine for A-Novel development and run the a-novel CLI.",
  // Served as the a-novel-kit/stack project site on GitHub Pages.
  base: "/stack/",
  cleanUrls: true,
  markdown: {
    config: (md) => {
      md.use(tabsMarkdownPlugin);
    },
  },
  themeConfig: {
    nav: [
      { text: "Dependencies", link: "/dependencies/" },
      { text: "a-novel CLI", link: "/cli/" },
    ],
    sidebar: [
      {
        text: "Dependencies",
        items: [
          { text: "Overview", link: "/dependencies/" },
          { text: "Git", link: "/dependencies/git" },
          { text: "GitHub", link: "/dependencies/github" },
          { text: "Commit signing", link: "/dependencies/signing" },
          { text: "Go", link: "/dependencies/go" },
          { text: "Node.js and pnpm", link: "/dependencies/node" },
          { text: "Podman", link: "/dependencies/podman" },
          { text: "Discord", link: "/dependencies/discord" },
          { text: "Bitwarden (maintainers)", link: "/dependencies/bitwarden" },
          { text: "AI agents (optional)", link: "/dependencies/ai-agents" },
        ],
      },
      {
        text: "a-novel CLI",
        items: [
          { text: "Install and set up", link: "/cli/" },
          { text: "Use the terminal UI", link: "/cli/ui" },
          { text: "Run services", link: "/cli/services" },
          { text: "Test and build", link: "/cli/test-build" },
          { text: "Manage secrets", link: "/cli/secrets" },
          { text: "Back up service data", link: "/cli/volumes" },
          { text: "Work in parallel stacks", link: "/cli/stacks" },
          { text: "Comment as the bot", link: "/cli/bot-comments" },
          { text: "Configure repositories", link: "/cli/repositories" },
        ],
      },
    ],
    // List sub-sections too, so a reader can jump past a step already done.
    outline: [2, 3],
    search: { provider: "local" },
    editLink: {
      pattern: "https://github.com/a-novel-kit/stack/edit/master/docs/:path",
    },
    socialLinks: [
      { icon: "github", link: "https://github.com/a-novel-kit/stack" },
      { icon: "discord", link: "https://discord.gg/rp4Qr8cA" },
    ],
  },
});

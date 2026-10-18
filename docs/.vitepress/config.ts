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
      { text: "Dependencies", link: "/dependencies/", activeMatch: "^/dependencies/" },
      { text: "a-novel CLI", link: "/cli/", activeMatch: "^/cli/$" },
      { text: "Working with AI", link: "/ai/", activeMatch: "^/ai/" },
      { text: "Runbooks", link: "/cli/ui", activeMatch: "^/cli/.+" },
      { text: "Sysadmins", link: "/sysadmins/", activeMatch: "^/sysadmins/" },
    ],
    sidebar: [
      {
        text: "Dependencies",
        items: [
          { text: "Overview", link: "/dependencies/" },
          { text: "Discord", link: "/dependencies/discord" },
          { text: "Git", link: "/dependencies/git" },
          { text: "GitHub", link: "/dependencies/github" },
          { text: "Commit signing", link: "/dependencies/signing" },
          { text: "Go", link: "/dependencies/go" },
          { text: "Node.js and pnpm", link: "/dependencies/node" },
          { text: "Podman", link: "/dependencies/podman" },
        ],
      },
      {
        text: "a-novel CLI",
        items: [{ text: "Install and set up", link: "/cli/" }],
      },
      {
        text: "Working with AI",
        items: [
          { text: "Set up an agent", link: "/ai/setup" },
          { text: "Work with an agent", link: "/ai/" },
        ],
      },
      {
        text: "Runbooks",
        items: [
          { text: "Use the terminal UI", link: "/cli/ui" },
          { text: "Run services", link: "/cli/services" },
          { text: "Test and build", link: "/cli/test-build" },
          { text: "Manage secrets", link: "/cli/secrets" },
          { text: "Back up service data", link: "/cli/volumes" },
          { text: "Work in parallel stacks", link: "/cli/stacks" },
          { text: "Comment as the bot", link: "/cli/bot-comments" },
        ],
      },
      {
        text: "Sysadmins",
        items: [
          { text: "Requirements and duties", link: "/sysadmins/" },
          { text: "Google Cloud CLI", link: "/sysadmins/gcloud" },
          { text: "OpenTofu", link: "/sysadmins/opentofu" },
          { text: "Bitwarden", link: "/sysadmins/bitwarden" },
          { text: "Configure repositories", link: "/sysadmins/repositories" },
          { text: "Create a repository", link: "/sysadmins/create-repository" },
          { text: "Add a member", link: "/sysadmins/add-member" },
          { text: "Promote a member", link: "/sysadmins/promote-member" },
          { text: "Offboard a member", link: "/sysadmins/offboard-member" },
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

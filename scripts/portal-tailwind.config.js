// 门户单文件前端的 Tailwind 配置。
//
// 只有 scripts/portal-css.sh 会用到它：那个脚本把编译结果内联回 index.html，
// 所以部署时不需要 Node、也不需要联网。改这里的主题要重新跑一次脚本。

module.exports = {
  darkMode: "class",
  content: ["./internal/portal/static/index.html"],
  theme: {
    extend: {
      colors: {
        bg: "var(--bg)",
        surface: "var(--surface)",
        glass: "var(--glass)",
        ink: "var(--ink)",
        ink2: "var(--ink2)",
        ink3: "var(--ink3)",
        line: "var(--line)",
        line2: "var(--line2)",
        accent: "var(--accent)",
        accent2: "var(--accent2)",
        good: "var(--good)",
        bad: "var(--bad)",
        warn: "var(--warn)",
      },
      borderRadius: { card: "12px" },
      boxShadow: { card: "var(--shadow)", pop: "var(--shadow-pop)" },
      fontFamily: {
        sans: [
          "Inter", "system-ui", "-apple-system", "BlinkMacSystemFont",
          '"PingFang SC"', '"Microsoft YaHei"', "sans-serif",
        ],
      },
    },
  },
  plugins: [],
};

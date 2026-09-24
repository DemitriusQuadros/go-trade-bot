import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';

// MarkdownMessage renders an AgentRun.response_text - the model answers in
// markdown (headings, bold, lists - see any real "### Strategy Details"
// summary the copilot produces), and until now that was dumped as raw text
// with literal `**`/`###`/`-` characters instead of being rendered. Every
// element gets its own small, terminal-styled override here rather than
// pulling in @tailwindcss/typography's "prose" classes, since this app's
// theme (Fira Code monospace, black/green, no serif prose styling anywhere
// else) would clash with typography's default look.
export function MarkdownMessage({ content }: { content: string }) {
  return (
    <div className="text-xs leading-relaxed break-words">
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        components={{
          p: ({ children }) => <p className="mb-2 last:mb-0">{children}</p>,
          h1: ({ children }) => <h1 className="text-sm font-bold text-green-300 mt-3 mb-1.5 first:mt-0">{children}</h1>,
          h2: ({ children }) => <h2 className="text-sm font-bold text-green-300 mt-3 mb-1.5 first:mt-0">{children}</h2>,
          h3: ({ children }) => <h3 className="text-xs font-bold text-green-300 mt-3 mb-1 first:mt-0 uppercase tracking-wide">{children}</h3>,
          h4: ({ children }) => <h4 className="text-xs font-bold text-green-400 mt-2 mb-1 first:mt-0">{children}</h4>,
          ul: ({ children }) => <ul className="list-disc list-outside pl-4 mb-2 space-y-0.5">{children}</ul>,
          ol: ({ children }) => <ol className="list-decimal list-outside pl-4 mb-2 space-y-0.5">{children}</ol>,
          li: ({ children }) => <li className="marker:text-green-700">{children}</li>,
          strong: ({ children }) => <strong className="font-bold text-green-200">{children}</strong>,
          em: ({ children }) => <em className="italic text-green-400">{children}</em>,
          a: ({ href, children }) => (
            <a href={href} target="_blank" rel="noreferrer" className="text-emerald-400 hover:text-emerald-300 underline underline-offset-2">
              {children}
            </a>
          ),
          code: ({ children, className }) => {
            const isBlock = /language-/.test(className || '');
            return isBlock ? (
              <code className={className}>{children}</code>
            ) : (
              <code className="bg-black/60 border border-green-950/60 rounded px-1 py-0.5 text-emerald-300 font-mono">{children}</code>
            );
          },
          pre: ({ children }) => (
            <pre className="bg-black/60 border border-green-950/60 rounded p-2 mb-2 overflow-x-auto font-mono text-[11px]">{children}</pre>
          ),
          blockquote: ({ children }) => (
            <blockquote className="border-l-2 border-green-800/60 pl-2 mb-2 text-green-500 italic">{children}</blockquote>
          ),
          hr: () => <hr className="border-green-950/60 my-2" />,
          table: ({ children }) => (
            <div className="overflow-x-auto mb-2">
              <table className="border-collapse text-xs w-full">{children}</table>
            </div>
          ),
          th: ({ children }) => <th className="border border-green-950/60 px-2 py-1 text-left text-green-300 bg-black/40">{children}</th>,
          td: ({ children }) => <td className="border border-green-950/60 px-2 py-1">{children}</td>,
        }}
      >
        {content}
      </ReactMarkdown>
    </div>
  );
}

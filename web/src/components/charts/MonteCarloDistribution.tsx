import React from 'react';
import {
  BarChart,
  Bar,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ResponsiveContainer,
} from 'recharts';

interface MonteCarloDistributionProps {
  distribution: Array<{ bin: number; count: number }>;
  mean: number;
  var95: number;
  ruinProb: number;
  height?: number;
}

export function MonteCarloDistribution({
  distribution,
  mean,
  var95,
  ruinProb,
  height = 220,
}: MonteCarloDistributionProps) {
  if (!distribution || distribution.length === 0) {
    return (
      <div className="flex items-center justify-center p-8 text-xs text-slate-500 bg-slate-900/50 rounded-lg border border-slate-800">
        No Monte Carlo simulation data available.
      </div>
    );
  }

  const formatPercent = (v: number) => `${v.toFixed(1)}%`;

  return (
    <div className="w-full flex flex-col gap-3">
      <div className="grid grid-cols-3 gap-2 text-center text-xs">
        <div className="p-2 bg-slate-900/60 rounded border border-slate-800">
          <span className="text-slate-400 block text-[10px] uppercase">Mean Return</span>
          <span className={`font-mono font-bold ${mean >= 0 ? 'text-emerald-400' : 'text-rose-400'}`}>
            {mean.toFixed(2)}%
          </span>
        </div>
        <div className="p-2 bg-slate-900/60 rounded border border-slate-800">
          <span className="text-slate-400 block text-[10px] uppercase">95% VaR</span>
          <span className="font-mono font-bold text-amber-400">
            {var95.toFixed(2)}%
          </span>
        </div>
        <div className="p-2 bg-slate-900/60 rounded border border-slate-800">
          <span className="text-slate-400 block text-[10px] uppercase">Ruin Probability</span>
          <span className={`font-mono font-bold ${ruinProb > 0.05 ? 'text-rose-400' : 'text-slate-200'}`}>
            {(ruinProb * 100).toFixed(2)}%
          </span>
        </div>
      </div>

      <ResponsiveContainer width="100%" height={height}>
        <BarChart data={distribution} margin={{ top: 10, right: 10, left: -10, bottom: 5 }}>
          <CartesianGrid strokeDasharray="3 3" stroke="#243247" />
          <XAxis
            dataKey="bin"
            tickFormatter={formatPercent}
            stroke="#64748b"
            fontSize={10}
          />
          <YAxis stroke="#64748b" fontSize={10} />
          <Tooltip
            formatter={(val: any) => [val, 'Simulations']}
            labelFormatter={(bin: any) => `Return: ${bin.toFixed(1)}%`}
            contentStyle={{
              backgroundColor: '#162032',
              borderColor: '#243247',
              borderRadius: '0.5rem',
              color: '#f8fafc',
              fontSize: '12px',
            }}
          />
          <Bar dataKey="count" fill="#3b82f6" radius={[2, 2, 0, 0]} />
        </BarChart>
      </ResponsiveContainer>
    </div>
  );
}

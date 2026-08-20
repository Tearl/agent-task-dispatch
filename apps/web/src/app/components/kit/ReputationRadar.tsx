import { PolarAngleAxis, PolarGrid, Radar, RadarChart, ResponsiveContainer } from "recharts";

export interface ReputationDims {
  quality: number;
  speed: number;
  reliability: number;
  communication: number;
  compliance: number;
}

const LABELS: Record<keyof ReputationDims, string> = {
  quality: "交付质量",
  speed: "响应速度",
  reliability: "稳定性",
  communication: "沟通协作",
  compliance: "合规守约",
};

export function ReputationRadar({
  dims,
  color = "#22d3ee",
  height = 220,
}: {
  dims: ReputationDims;
  color?: string;
  height?: number;
}) {
  const data = (Object.keys(dims) as (keyof ReputationDims)[]).map((key) => ({
    dim: LABELS[key],
    value: dims[key],
  }));

  return (
    <div style={{ width: "100%", height }}>
      <ResponsiveContainer>
        <RadarChart data={data} outerRadius="72%">
          <PolarGrid stroke="rgba(120,170,220,0.18)" />
          <PolarAngleAxis dataKey="dim" tick={{ fill: "#8fa3c0", fontSize: 11 }} />
          <Radar
            dataKey="value"
            stroke={color}
            fill={color}
            fillOpacity={0.28}
            strokeWidth={2}
          />
        </RadarChart>
      </ResponsiveContainer>
    </div>
  );
}

// Barrel for the shared hand-rolled SVG chart primitives. Consumers:
// `import { StatCard, DonutChart, BarChart, TimeSeriesChart, ChartCard } from '@shared/charts'`.
// Every component is dependency-free (plain React + SVG) and themed through the
// --ui-* design tokens each app already defines.
export { StatCard } from './StatCard';
export { DonutChart } from './DonutChart';
export { BarChart } from './BarChart';
export { TimeSeriesChart } from './TimeSeriesChart';
export { ChartCard } from './ChartCard';
export { colorFor, CATEGORICAL, SEVERITY_COLORS, CATEGORY_COLORS } from './palette';

// What a NAS run cost: each test's time and agent model calls from the e2e
// report, and the LLM requests the model recorded (see llm.ts).
import { basename } from 'node:path';

type Step = { api?: string; metrics?: { modelCalls?: number } };
type Result = { titlePath: string[]; file: string; status: string; attempts: { durationMs: number; steps: Step[] }[] };
type Call = { reasoningEffort: string | null; inputTokens: number | null; cachedTokens: number | null; outputTokens: number | null };

const sum = (values: (number | null | undefined)[]) => values.reduce<number>((total, value) => total + (value ?? 0), 0);

export function summarize(report: { run: { results: Result[] } }, calls: Call[]) {
  return {
    tests: report.run.results.map(result => {
      const steps = result.attempts.flatMap(attempt => attempt.steps);
      return {
        file: basename(result.file), title: result.titlePath.join(' › '), status: result.status,
        seconds: Math.round(sum(result.attempts.map(attempt => attempt.durationMs)) / 1000),
        agentCalls: sum(steps.map(step => step.metrics?.modelCalls)),
      };
    }),
    llm: {
      requests: calls.length, withoutMaxEffort: calls.filter(call => call.reasoningEffort !== 'max').length,
      inputTokens: sum(calls.map(call => call.inputTokens)), cachedTokens: sum(calls.map(call => call.cachedTokens)),
      outputTokens: sum(calls.map(call => call.outputTokens)),
    },
  };
}

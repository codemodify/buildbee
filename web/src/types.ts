export type Person = {
  id: string;
  name: string;
  created_at: string;
};

export type Member = {
  id: string;
  project_id: string;
  person_id?: string;
  kind: "human" | "bot" | string;
  display_name: string;
  role: string;
  instructions?: string;
  agent?: string;
};

export type Channel = {
  id: string;
  project_id: string;
  name: string;
  archived_at?: string;
};

export type Project = {
  id: string;
  name: string;
  auto_run?: boolean;
  merge_policy?: "auto" | "approval" | string;
  max_runs?: number;
  instructions?: string;
  repo_url?: string;
  default_branch?: string;
  archived_at?: string;
  members?: Member[];
  channels?: Channel[];
};

export type Message = {
  id: string;
  seq: number;
  channel_id: string;
  member_id: string;
  body: string;
  created_at: string;
};

export type Task = {
  id: string;
  project_id: string;
  title: string;
  body?: string;
  status: string;
  assignee_member_id?: string;
  created_by_member_id?: string;
  issue_number?: number;
  issue_url?: string;
  branch?: string;
  pr_url?: string;
  merged_at?: string;
};

export type Handoff = {
  id: string;
  task_id: string;
  from_member_id: string;
  to_member_id: string;
  note: string;
  status: string;
  created_at: string;
};

export type Decision = {
  id: string;
  project_id: string;
  task_id?: string;
  prompt: string;
  options: string[];
  recommendation: string;
  answer?: string;
  reused?: boolean;
  assignee_id?: string;
  action?: string;
};

export type Run = {
  id: string;
  task_id: string;
  bot_member_id?: string;
  status: string;
  kind?: "plan" | "build" | "review" | "merge" | string;
  detail: string;
  summary?: string;
  branch?: string;
  pr_url?: string;
  verdict?: string;
  context_tokens?: number;
  context_size?: number;
  cost?: number;
  cost_currency?: string;
  agent?: string;
  worker?: string;
  attempts?: number;
  started_at?: string;
  finished_at?: string;
};

export type RunEvent = {
  id: string;
  run_id: string;
  seq: number;
  kind: "token" | "thought" | "plan" | "tool_call" | "tool_result" | "usage" | "status" | "log" | "steer" | string;
  payload: Record<string, unknown>;
  created_at: string;
};

export type Artifact = {
  id: string;
  kind: string;
  name: string;
  body?: string;
  size: number;
  url?: string;
  run_id?: string;
};

export type Pipeline = {
  id: string;
  name: string;
  status: string;
  external_url?: string;
};

export type TaskDetail = Task & {
  handoffs: Handoff[];
  runs: Run[];
  artifacts: Artifact[];
  pipelines: Pipeline[];
};

export type Preferences = {
  person_id: string;
  mute_mentions: boolean;
  mute_routines: boolean;
};

export type Routine = {
  id: string;
  name: string;
  prompt?: string;
  bot_member_id?: string;
  last_task_id?: string;
  schedule: string;
  enabled: boolean;
};

export type Activity = {
  seq: number;
  actor: string;
  actor_member_id?: string;
  type: string;
  action: string;
  subject_id?: string;
  payload: Record<string, unknown>;
  created_at: string;
};

export type Notification = {
  id: string;
  seq: number;
  project_id: string;
  person_id: string;
  kind: string;
  title: string;
  body?: string;
  href?: string;
  read_at?: string | null;
  created_at: string;
};

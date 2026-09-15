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
};

export type Run = {
  id: string;
  task_id: string;
  bot_member_id?: string;
  status: string;
  detail: string;
};

export type RunEvent = {
  id: string;
  run_id: string;
  seq: number;
  kind: "token" | "tool_call" | "tool_result" | "status" | "log" | string;
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

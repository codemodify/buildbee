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
  left_at?: string;
};

export type Channel = {
  id: string;
  project_id: string;
  name: string;
  kind?: "channel" | "dm" | string;
  locked?: boolean;
  member_ids?: string[];
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
  agent_image?: string;
  agent_permissions?: "auto" | "ask" | string;
  kind?: "project" | "direct" | string; // direct: the space for DMs between people
  archived_at?: string;
  members?: Member[];
  channels?: Channel[];
};

export type Message = {
  id: string;
  seq: number;
  channel_id: string;
  project_id?: string;
  member_id: string;
  body: string;
  thread_id?: string;
  reply_count?: number;
  last_reply_at?: string;
  task_id?: string;
  edited_at?: string;
  deleted_at?: string;
  created_at: string;
  files?: FileRef[];
};

/** FileRef is an attachment; url downloads it. */
export type FileRef = {
  id: string;
  name: string;
  content_type: string;
  size: number;
  url: string;
};

export type Thread = {
  root: Message;
  replies: Message[];
  has_more: boolean;
};

export type Task = {
  id: string;
  project_id: string;
  title: string;
  body?: string;
  status: "open" | "in_progress" | "done" | "canceled" | string;
  assignee_member_id?: string;
  created_by_member_id?: string;
  issue_number?: number;
  issue_url?: string;
  branch?: string;
  head_commit?: string;
  pr_url?: string;
  merged_at?: string;
  thread_id?: string;
  created_at?: string;
  updated_at?: string;
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
  commit?: string;
  created_at?: string;
};

export type RunKind = "plan" | "build" | "review" | "merge";

export type Run = {
  id: string;
  task_id: string;
  project_id?: string;
  bot_member_id?: string;
  status: "pending" | "running" | "succeeded" | "failed" | "canceled" | string;
  kind?: RunKind | string;
  detail: string;
  summary?: string;
  branch?: string;
  commit?: string;
  pr_url?: string;
  verdict?: string;
  context_tokens?: number;
  context_size?: number;
  cost?: number;
  cost_currency?: string;
  agent?: string;
  worker?: string;
  attempts?: number;
  created_at?: string;
  started_at?: string;
  finished_at?: string;
};

/** RunRow is a Run on the Server's dashboard, with what it is about. */
export type RunRow = Run & {
  task_title: string;
  project_name: string;
  bot_name?: string;
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
  created_at?: string;
  truncated?: boolean;
  raw_url?: string;
};

export type Pipeline = {
  id: string;
  name: string;
  status: string;
  commit?: string;
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
  schedule: string;
  enabled: boolean;
  bot_member_id?: string;
  last_run_at?: string;
  last_task_id?: string;
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

export type Unread = {
  channel_id: string;
  unread: number;
  last_seq: number;
};

/** BotSeen is a Bot whose agent is connected: what it runs and where. */
export type BotSeen = {
  bot_id: string;
  name: string;
  project_id: string;
  ai: string;
  process: string;
  host?: string;
  slots?: number;
  running: number;
  last_seen: string;
};

/** BotWork is how many Runs a Bot has going and waiting. */
export type BotWork = { running: number; queued: number };

export type Presence = {
  people: Person[];
  bots: BotSeen[];
  /** Work per Bot ID; "" is work nobody is assigned, such as a merge. */
  work: Record<string, BotWork>;
  slots: number;
  running: number;
};

export type UsageRow = {
  key: string;
  name?: string;
  runs: number;
  cost: Record<string, number>;
  context_tokens: number;
};

export type Usage = {
  since: string;
  runs: number;
  cost: Record<string, number>;
  by_agent: UsageRow[];
  by_project?: UsageRow[];
};

export type Membership = {
  project_id: string;
  project_name: string;
  member_id: string;
  role: string;
  joined_at: string;
  left_at?: string;
};

export type Roster = {
  people: (Person & { projects: Membership[] })[];
  bots: (Member & { project_name: string })[];
  events: { name: string; action: string; project_id: string; project_name: string; at: string }[];
};

/** DM is a direct message between people, as its reader sees it. */
export type DM = Channel & {
  with: { member_id: string; person_id?: string; name: string; kind: string; role: string }[];
  unread: number;
  last_seq: number;
  last_at: string;
};

/** SearchHit is a message found by search, with where it is. */
export type SearchHit = Message & {
  channel_name: string;
  channel_kind: string;
  project_name?: string;
  author_name: string;
  author_kind: string;
};

/** BotTemplate is one of the autopilot's Bots, offered when adding a Bot. */
export type BotTemplate = { name: string; role: string; instructions: string };

/** A message as posted, with what its mentions set in motion. */
export type Posted = Message & {
  tasks?: Task[];
};

/** A frame on the multi-topic WebSocket. */
export type Frame = {
  topic: string;
  cursor: number;
  type: "message" | "run_event" | "activity" | "notification" | "presence" | string;
  data: unknown;
};

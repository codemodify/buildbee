export type Member = {
  id: string;
  project_id: string;
  kind: "human" | "bot" | string;
  display_name: string;
  role: string;
  instructions?: string;
  identity: string;
};

export type Channel = {
  id: string;
  project_id: string;
  name: string;
};

export type Project = {
  id: string;
  name: string;
  auto_run?: boolean;
  members?: Member[];
  channels?: Channel[];
};

export type Message = {
  id: string;
  channel_id: string;
  member_id: string;
  body: string;
  created_at: string;
};

export type Task = {
  id: string;
  project_id: string;
  title: string;
  status: string;
  assignee_member_id?: string;
  issue_number?: number;
  issue_url?: string;
};

export type Decision = {
  id: string;
  project_id: string;
  prompt: string;
  options: string[];
  recommendation: string;
  answer?: string;
  reused?: boolean;
};

export type Run = {
  id: string;
  task_id: string;
  status: string;
  detail: string;
};

export type Artifact = {
  id: string;
  kind: string;
  name: string;
  body?: string;
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
  runs: Run[];
  artifacts: Artifact[];
  pipelines: Pipeline[];
};

export type AuthMe = {
  mode: string;
  oauth: boolean;
  signed_in: boolean;
  dev: boolean;
  identity?: { display_name?: string; github_login?: string };
};

export type Routine = {
  id: string;
  name: string;
  schedule: string;
  enabled: boolean;
};

export type Notification = {
  id: string;
  project_id: string;
  member_id: string;
  kind: string;
  title: string;
  body?: string;
  href?: string;
  read_at?: string | null;
  created_at: string;
};

export type Member = {
  id: string;
  project_id: string;
  kind: "human" | "bot" | string;
  display_name: string;
  role: string;
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
};

export type Decision = {
  id: string;
  project_id: string;
  prompt: string;
  options: string[];
  recommendation: string;
  answer?: string;
};

//! Central root view: owns all shared state, the refresh loops, the
//! sidebar, and the root layout. Feature modules in `features/` extend
//! `RootView` with per-destination methods and import the shared
//! formatting helpers from this file.

mod features;

use std::time::Duration;

use gpui_kit::component::{
    Icon, IconName, h_flex, sidebar::{Sidebar, SidebarMenu, SidebarMenuItem},
    v_flex,
};
use gpui_kit::prelude::*;
use gpui_kit::*;

use crate::navigation::Panel;
use crate::services::dashboard::{load_deep, load_interactive, DeepModel, InteractiveModel};
use crate::services::deep_run::{list, MissionPreview, RunRecord};
use crate::services::interactive::{load_status, DeadlinePreview, StartPreview, Snapshot};
use crate::services::state::{load_latest, DeepState};

pub struct RootView {
    state: Option<DeepState>,
    refresh_error: Option<String>,
    selected_task: Option<String>,
    panel: Panel,
    interactive_status: Option<Snapshot>,
    interactive_error: Option<String>,
    selected_hours: f64,
    pending_action: Option<PendingAction>,
    operation_busy: bool,
    operation_label: Option<String>,
    operation_message: Option<(bool, String)>,
    run_history: Vec<RunRecord>,
    selected_run: Option<String>,
    run_history_error: Option<String>,
    mission_path: String,
    repo_path: String,
    mission_preview: Option<MissionPreview>,
    run_hours: f64,
    run_busy: bool,
    run_message: Option<(bool, String)>,
    pending_run: Option<StartPreview>,
    confirm_stop: bool,
    confirm_resume: Option<String>,
    interactive_dash: Option<InteractiveModel>,
    interactive_dash_error: Option<String>,
    deep_dash: Option<DeepModel>,
    deep_dash_error: Option<String>,
}

pub enum PendingAction {
    Start(StartPreview),
    Deadline { duration: String, preview: DeadlinePreview },
    Resume(i64),
    Destroy(i64),
}

impl RootView {
    pub fn new() -> Self {
        let mut view = Self {
            state: None,
            refresh_error: None,
            selected_task: None,
            panel: Panel::DeepWork,
            interactive_status: None,
            interactive_error: None,
            selected_hours: 1.0,
            pending_action: None,
            operation_busy: false,
            operation_label: None,
            operation_message: None,
            run_history: Vec::new(),
            selected_run: None,
            run_history_error: None,
            mission_path: String::new(),
            repo_path: String::new(),
            mission_preview: None,
            run_hours: 1.0,
            run_busy: false,
            run_message: None,
            pending_run: None,
            confirm_stop: false,
            confirm_resume: None,
            interactive_dash: None,
            interactive_dash_error: None,
            deep_dash: None,
            deep_dash_error: None,
        };
        view.apply_load(load_latest());
        view
    }

    fn apply_run_history(&mut self, result: Result<Vec<RunRecord>, String>) {
        match result {
            Ok(history) => {
                if self.selected_run.as_ref().is_none_or(|id| !history.iter().any(|r| &r.state.session_id == id)) {
                    self.selected_run = history.iter().find(|r| r.latest).map(|r| r.state.session_id.clone());
                }
                self.run_history = history;
                self.run_history_error = None;
            }
            Err(error) => { self.run_history.clear(); self.selected_run = None; self.run_history_error = Some(error); },
        }
    }

    pub fn start_refresh(&mut self, cx: &mut Context<Self>) {
        let view = cx.entity().downgrade();
        cx.spawn(async move |_, cx| loop {
            smol::Timer::after(Duration::from_secs(5)).await;
            let result = smol::unblock(load_latest).await;
            if view
                .update(cx, |this, cx| {
                    this.apply_load(result);
                    cx.notify();
                })
                .is_err()
            {
                break;
            }
        })
        .detach();

        let view = cx.entity().downgrade();
        cx.spawn(async move |_, cx| {
            let mut refresh_tick = 0u8;
            loop {
                smol::Timer::after(Duration::from_secs(3)).await;
                refresh_tick = refresh_tick.wrapping_add(1);
                let refresh = refresh_tick >= 4;
                if refresh {
                    refresh_tick = 0;
                }
                let result = smol::unblock(move || load_status(refresh)).await;
                if view
                    .update(cx, |this, cx| {
                        match result {
                            Ok(snapshot) => {
                                this.interactive_status = Some(snapshot);
                                this.interactive_error = None;
                            }
                            Err(error) => this.interactive_error = Some(error),
                        }
                        cx.notify();
                    })
                    .is_err()
                {
                    break;
                }
            }
        })
        .detach();

        let view = cx.entity().downgrade();
        cx.spawn(async move |_, cx| {
            let result = smol::unblock(list).await;
            if view.update(cx, |this, cx| { this.apply_run_history(result); cx.notify(); }).is_err() { return; }
            loop {
                smol::Timer::after(Duration::from_secs(5)).await;
                let result = smol::unblock(list).await;
                if view.update(cx, |this, cx| { this.apply_run_history(result); cx.notify(); }).is_err() { break; }
            }
        }).detach();

        // Interactive compute/GPU dashboard (mirrors `stint dashboard`).
        let view = cx.entity().downgrade();
        cx.spawn(async move |_, cx| {
            loop {
                let result = smol::unblock(load_interactive).await;
                if view.update(cx, |this, cx| {
                    this.apply_interactive_dash(result);
                    cx.notify();
                }).is_err() { break; }
                smol::Timer::after(Duration::from_secs(5)).await;
            }
        }).detach();

        // Deep Work dashboard (mirrors `stint deep dash`).
        let view = cx.entity().downgrade();
        cx.spawn(async move |_, cx| {
            loop {
                let result = smol::unblock(load_deep).await;
                if view.update(cx, |this, cx| {
                    this.apply_deep_dash(result);
                    cx.notify();
                }).is_err() { break; }
                smol::Timer::after(Duration::from_secs(5)).await;
            }
        }).detach();
    }

    fn apply_load(&mut self, result: Result<DeepState, String>) {
        match result {
            Ok(state) => {
                if !state
                    .tasks
                    .iter()
                    .any(|task| Some(&task.id) == self.selected_task.as_ref())
                {
                    self.selected_task = state.tasks.first().map(|task| task.id.clone());
                }
                self.state = Some(state);
                self.refresh_error = None;
            }
            Err(error) => self.refresh_error = Some(error),
        }
    }

    fn apply_interactive_dash(&mut self, result: Result<InteractiveModel, String>) {
        match result {
            Ok(model) => {
                self.interactive_dash = Some(model);
                self.interactive_dash_error = None;
            }
            Err(error) => self.interactive_dash_error = Some(error),
        }
    }

    fn apply_deep_dash(&mut self, result: Result<DeepModel, String>) {
        match result {
            Ok(model) => {
                self.deep_dash = Some(model);
                self.deep_dash_error = None;
            }
            Err(error) => self.deep_dash_error = Some(error),
        }
    }

    fn sidebar(&mut self, cx: &mut Context<Self>) -> impl IntoElement {
        let run_count = self.run_history.len().to_string();
        let menu = SidebarMenu::new()
            .child(
                SidebarMenuItem::new("Deep Work")
                    .icon(Icon::new(IconName::Bot))
                    .active(self.panel == Panel::DeepWork)
                    .on_click(cx.listener(|this, _, _, cx| {
                        this.panel = Panel::DeepWork;
                        cx.notify();
                    })),
            )
            .child(
                SidebarMenuItem::new("Deep Run")
                    .icon(Icon::new(IconName::Play))
                    .active(self.panel == Panel::DeepRun)
                    .suffix(move |_, _| {
                        div()
                            .px_2()
                            .py_1()
                            .rounded_md()
                            .bg(rgb(0x252a34))
                            .text_color(rgb(0x9ba6b8))
                            .child(run_count.clone())
                            .into_any_element()
                    })
                    .on_click(cx.listener(|this, _, _, cx| {
                        this.panel = Panel::DeepRun;
                        cx.notify();
                    })),
            )
            .child(
                SidebarMenuItem::new("Interactive")
                    .icon(Icon::new(IconName::SquareTerminal))
                    .active(self.panel == Panel::Interactive)
                    .on_click(cx.listener(|this, _, _, cx| {
                        this.panel = Panel::Interactive;
                        cx.notify();
                    })),
            )
            .child(
                SidebarMenuItem::new("Live Dashboard")
                    .icon(Icon::new(IconName::Cpu))
                    .active(self.panel == Panel::InteractiveDash)
                    .on_click(cx.listener(|this, _, _, cx| {
                        this.panel = Panel::InteractiveDash;
                        cx.notify();
                    })),
            )
            .child(
                SidebarMenuItem::new("Deep Board")
                    .icon(Icon::new(IconName::LayoutDashboard))
                    .active(self.panel == Panel::DeepBoard)
                    .on_click(cx.listener(|this, _, _, cx| {
                        this.panel = Panel::DeepBoard;
                        cx.notify();
                    })),
            );
        Sidebar::new("stint-nav")
            .w(px(232.))
            .header(
                div()
                    .px_2()
                    .py_1()
                    .text_base()
                    .font_weight(FontWeight::BOLD)
                    .child("Stint"),
            )
            .child(menu)
            .footer(self.compute_footer())
    }

    fn compute_footer(&self) -> impl IntoElement {
        let (dot, label, sub) = match &self.interactive_status {
            Some(snapshot) if snapshot.active => {
                let status = snapshot.session.status.clone();
                let dot = match status.as_str() {
                    "READY" => rgb(0x67c587),
                    "RECOVERABLE" => rgb(0xe6ad69),
                    "STOPPED" => rgb(0x8993a4),
                    _ => rgb(0x70a4e5),
                };
                let sub = format!(
                    "{} · {}",
                    truncate(&status, 20),
                    format_duration(snapshot.time.remaining_seconds)
                );
                (dot, format!("Compute · #{}", snapshot.session.instance_id), sub)
            }
            Some(_) => (rgb(0x8993a4), "No compute session".to_string(), "rent when needed".to_string()),
            None if self.interactive_error.is_some() => (
                rgb(0xe6ad69),
                "Status unavailable".to_string(),
                "retrying".to_string(),
            ),
            None => (rgb(0x8993a4), "Checking compute…".to_string(), String::new()),
        };
        v_flex()
            .gap_1()
            .p_3()
            .border_t_1()
            .border_color(rgb(0x252a34))
            .child(
                h_flex().items_center().gap_2()
                    .child(
                        div().size_2().rounded_full().bg(dot),
                    )
                    .child(div().text_sm().font_weight(FontWeight::MEDIUM).child(label)),
            )
            .when(!sub.is_empty(), |this| this.child(div().px_4().text_sm().text_color(rgb(0x8993a4)).child(sub)))
    }
}

fn present(value: &str) -> String {
    if value.is_empty() { "—".to_string() } else { value.to_string() }
}

fn format_duration(seconds: f64) -> String {
    if !seconds.is_finite() || seconds <= 0.0 {
        return "expired".to_string();
    }
    let total_minutes = (seconds / 60.0).ceil() as u64;
    let hours = total_minutes / 60;
    let minutes = total_minutes % 60;
    if hours > 0 {
        format!("{hours}h {minutes:02}m")
    } else {
        format!("{minutes}m")
    }
}

fn truncate(value: &str, max_chars: usize) -> String {
    let value = value.replace('\n', " ");
    if value.chars().count() <= max_chars {
        return value;
    }
    format!("{}…", value.chars().take(max_chars).collect::<String>())
}

impl Render for RootView {
    fn render(&mut self, _window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let content = match self.panel {
            Panel::DeepWork => {
                v_flex().flex_1().min_w_0().h_full().p_5().gap_4()
                    .child(self.session_overview())
                    .child(self.work_area(cx))
                    .into_any_element()
            }
            Panel::DeepRun => v_flex().flex_1().min_w_0().h_full().p_5().child(self.deep_run_panel(cx)).into_any_element(),
            Panel::Interactive => v_flex().flex_1().min_w_0().h_full().p_5().child(self.interactive_panel(cx)).into_any_element(),
            Panel::InteractiveDash => v_flex().flex_1().min_w_0().h_full().p_5().child(self.interactive_dash_panel()).into_any_element(),
            Panel::DeepBoard => v_flex().flex_1().min_w_0().h_full().p_5().child(self.deep_board_panel()).into_any_element(),
        };
        h_flex()
            .w_full()
            .h_full()
            .bg(rgb(0x111318))
            .text_color(rgb(0xe7e9ee))
            .child(self.sidebar(cx))
            .child(content)
    }
}

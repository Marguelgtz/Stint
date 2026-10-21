mod state;
mod interactive;
mod deep_run;
mod dashboard;

use std::time::Duration;

use gpui_kit::component::{
    Icon, IconName, Theme, ThemeMode, TitleBar, h_flex, sidebar::{Sidebar, SidebarMenu, SidebarMenuItem},
    v_flex,
};
use gpui_kit::prelude::*;
use gpui_kit::*;
use interactive::{DeadlinePreview, Snapshot, StartPreview};
use deep_run::{MissionPreview, RunRecord};
use state::{DeepState, Task};
use dashboard::{DeepModel, InteractiveModel};

actions!(stint_ui, [Quit]);

#[derive(Clone, Copy, PartialEq, Eq)]
enum Panel {
    DeepWork,
    DeepRun,
    Interactive,
    InteractiveDash,
    DeepBoard,
}

macro_rules! action_button {
    ($label:expr, $handler:expr) => {
        div().id(concat!("action-", $label)).px_3().py_2().rounded_md().cursor_pointer().bg(rgb(0x2b3543)).text_sm()
            .on_click($handler).child($label)
    };
}

fn main() {
    gpui_kit::application().with_assets(gpui_kit::assets::Assets).run(|cx| {
        gpui_kit::init(cx);
        cx.bind_keys([
            #[cfg(target_os = "macos")]
            KeyBinding::new("cmd-q", Quit, None),
            #[cfg(not(target_os = "macos"))]
            KeyBinding::new("alt-f4", Quit, None),
        ]);
        cx.on_action(|_: &Quit, cx: &mut App| cx.quit());

        let options = WindowOptions {
            window_bounds: Some(WindowBounds::centered(size(px(1040.), px(720.)), cx)),
            ..TitleBar::window_options()
        };

        gpui_kit::open_window(options, cx, |window, cx| {
            window.activate_window();
            window.set_window_title("Stint");
            Theme::change(ThemeMode::Dark, Some(window), cx);
            cx.new(|cx| {
                let mut view = RootView::new();
                view.start_refresh(cx);
                view
            })
        })
        .expect("failed to open window");
    });
}

struct RootView {
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

enum PendingAction {
    Start(StartPreview),
    Deadline { duration: String, preview: DeadlinePreview },
    Resume(i64),
    Destroy(i64),
}

impl RootView {
    fn new() -> Self {
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
        view.apply_load(state::load_latest());
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

    fn start_refresh(&mut self, cx: &mut Context<Self>) {
        let view = cx.entity().downgrade();
        cx.spawn(async move |_, cx| loop {
            smol::Timer::after(Duration::from_secs(5)).await;
            let result = smol::unblock(state::load_latest).await;
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
                let result = smol::unblock(move || interactive::load_status(refresh)).await;
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
            let result = smol::unblock(deep_run::list).await;
            if view.update(cx, |this, cx| { this.apply_run_history(result); cx.notify(); }).is_err() { return; }
            loop {
                smol::Timer::after(Duration::from_secs(5)).await;
                let result = smol::unblock(deep_run::list).await;
                if view.update(cx, |this, cx| { this.apply_run_history(result); cx.notify(); }).is_err() { break; }
            }
        }).detach();

        // Interactive compute/GPU dashboard (mirrors `stint dashboard`).
        let view = cx.entity().downgrade();
        cx.spawn(async move |_, cx| {
            loop {
                let result = smol::unblock(dashboard::load_interactive).await;
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
                let result = smol::unblock(dashboard::load_deep).await;
                if view.update(cx, |this, cx| {
                    this.apply_deep_dash(result);
                    cx.notify();
                }).is_err() { break; }
                smol::Timer::after(Duration::from_secs(5)).await;
            }
        }).detach();
    }

    fn pick_path(&mut self, mission: bool, cx: &mut Context<Self>) {
        let receiver = cx.prompt_for_paths(PathPromptOptions {
            files: mission,
            directories: !mission,
            multiple: false,
            prompt: Some(if mission { "Choose a Deep Work mission".into() } else { "Choose a clean target repository".into() }),
        });
        let view = cx.entity().downgrade();
        cx.spawn(async move |_, cx| {
            let result = receiver.await;
            let _ = view.update(cx, |this, cx| {
                if let Ok(Ok(Some(paths))) = result {
                    if let Some(path) = paths.first() {
                        if mission { this.mission_path = path.display().to_string(); this.mission_preview = None; }
                        else { this.repo_path = path.display().to_string(); this.mission_preview = None; }
                    }
                }
                cx.notify();
            });
        }).detach();
    }

    fn preview_mission(&mut self, cx: &mut Context<Self>) {
        let mission = self.mission_path.clone();
        let repo = self.repo_path.clone();
        self.run_busy = true;
        self.run_message = None;
        let view = cx.entity().downgrade();
        cx.spawn(async move |_, cx| {
            let result = smol::unblock(move || deep_run::preview(&mission, &repo)).await;
            let _ = view.update(cx, |this, cx| {
                this.run_busy = false;
                match result { Ok(preview) => { this.mission_preview = Some(preview); this.run_message = Some((true, "Mission and repository validated".to_string())); }, Err(error) => { this.mission_preview = None; this.run_message = Some((false, error)); } }
                cx.notify();
            });
        }).detach();
    }

    fn request_deep_start(&mut self, cx: &mut Context<Self>) {
        if self.run_busy || self.mission_preview.is_none() { return; }
        self.run_busy = true;
        self.run_message = Some((true, "Checking compute session and offers…".to_string()));
        let hours = self.run_hours;
        let view = cx.entity().downgrade();
        cx.spawn(async move |_, cx| {
            let result = match smol::unblock(move || interactive::load_status(false)).await {
                Ok(snapshot) if snapshot.active && snapshot.session.status == "READY" => Ok((true, None)),
                _ => smol::unblock(move || interactive::preview_start(hours)).await.map(|preview| (false, Some(preview))),
            };
            let _ = view.update(cx, |this, cx| {
                this.run_busy = false;
                match result {
                    Ok((true, _)) => this.pending_run = Some(StartPreview { hours, session_max_cost_usd: 0.0, max_hourly_usd: 0.0, model: String::new(), endpoint: String::new(), fallback_attempts: 0, selected: interactive::StartCandidate { offer_id: "existing-ready-session".into(), gpu_model: String::new(), location: String::new(), hourly_usd: 0.0, estimated_session_usd: 0.0, runtime: String::new(), context_tokens: 0, within_cost_ceiling: true }, candidates: vec![] }),
                    Ok((false, Some(preview))) => this.pending_run = Some(preview),
                    _ => this.run_message = Some((false, result.err().unwrap_or_else(|| "Unable to preview compute offer".into()))),
                }
                cx.notify();
            });
        }).detach();
    }

    fn confirm_deep_start(&mut self, cx: &mut Context<Self>) {
        let Some(review) = self.pending_run.take() else { return; };
        let mission = self.mission_path.clone();
        let repo = self.repo_path.clone();
        let provision = review.selected.offer_id != "existing-ready-session";
        self.run_busy = true;
        self.run_message = Some((true, "Starting Deep Work…".to_string()));
        let view = cx.entity().downgrade();
        cx.spawn(async move |_, cx| {
            let result = smol::unblock(move || {
                if provision { interactive::start(&review)?; }
                deep_run::start(&mission, &repo, if provision { Some(review.hours) } else { None })
            }).await;
            let _ = view.update(cx, |this, cx| {
                this.run_busy = false;
                match result { Ok(output) => { this.run_message = Some((true, if output.is_empty() { "Deep Work launch accepted".into() } else { output })); }, Err(error) => this.run_message = Some((false, error)) }
                cx.notify();
            });
        }).detach();
    }

    fn run_lifecycle_action(&mut self, resume: Option<String>, cx: &mut Context<Self>) {
        let action = resume.clone();
        self.confirm_stop = false;
        self.confirm_resume = None;
        self.run_busy = true;
        let view = cx.entity().downgrade();
        cx.spawn(async move |_, cx| {
            let result = smol::unblock(move || match action { Some(id) => deep_run::resume(&id), None => deep_run::stop() }).await;
            let _ = view.update(cx, |this, cx| { this.run_busy = false; this.run_message = Some(match result { Ok(output) => (true, output), Err(error) => (false, error) }); cx.notify(); });
        }).detach();
    }

    fn request_start_preview(&mut self, cx: &mut Context<Self>) {
        if self.operation_busy {
            return;
        }
        self.pending_action = None;
        self.operation_message = None;
        self.operation_busy = true;
        self.operation_label = Some("Searching current interactive offers…".to_string());
        let hours = self.selected_hours;
        let view = cx.entity().downgrade();
        cx.spawn(async move |_, cx| {
            let result = smol::unblock(move || interactive::preview_start(hours)).await;
            let _ = view.update(cx, |this, cx| {
                this.operation_busy = false;
                this.operation_label = None;
                match result {
                    Ok(preview) => this.pending_action = Some(PendingAction::Start(preview)),
                    Err(error) => this.operation_message = Some((false, error)),
                }
                cx.notify();
            });
        })
        .detach();
    }

    fn request_deadline_preview(&mut self, direction: &'static str, duration: &'static str, cx: &mut Context<Self>) {
        if self.operation_busy {
            return;
        }
        self.pending_action = None;
        self.operation_message = None;
        self.operation_busy = true;
        self.operation_label = Some("Preparing deadline preview…".to_string());
        let direction = direction.to_string();
        let duration = duration.to_string();
        let pending_duration = duration.clone();
        let view = cx.entity().downgrade();
        cx.spawn(async move |_, cx| {
            let result = smol::unblock(move || interactive::preview_deadline(&direction, &duration)).await;
            let _ = view.update(cx, |this, cx| {
                this.operation_busy = false;
                this.operation_label = None;
                match result {
                    Ok(preview) => {
                        this.pending_action = Some(PendingAction::Deadline { duration: pending_duration, preview });
                    }
                    Err(error) => this.operation_message = Some((false, error)),
                }
                cx.notify();
            });
        })
        .detach();
    }

    fn confirm_pending_action(&mut self, cx: &mut Context<Self>) {
        let Some(action) = self.pending_action.take() else {
            return;
        };
        self.operation_message = None;
        match action {
            PendingAction::Start(preview) => self.run_operation(
                "Starting interactive session…",
                move || interactive::start(&preview),
                cx,
            ),
            PendingAction::Deadline { duration, preview } => {
                let direction = preview.direction.clone();
                self.run_operation("Updating session deadline…", move || {
                    interactive::change_deadline(&direction, &duration, &preview)
                }, cx);
            }
            PendingAction::Resume(instance_id) => self.run_operation(
                "Resuming the saved instance…",
                move || interactive::resume(instance_id),
                cx,
            ),
            PendingAction::Destroy(instance_id) => self.run_operation(
                "Destroying the interactive instance…",
                move || interactive::destroy(instance_id),
                cx,
            ),
        }
    }

    fn run_operation(
        &mut self,
        label: &'static str,
        operation: impl FnOnce() -> Result<String, String> + Send + 'static,
        cx: &mut Context<Self>,
    ) {
        if self.operation_busy {
            return;
        }
        self.operation_busy = true;
        self.operation_label = Some(label.to_string());
        let view = cx.entity().downgrade();
        cx.spawn(async move |_, cx| {
            let result = smol::unblock(operation).await;
            let _ = view.update(cx, |this, cx| {
                this.operation_busy = false;
                this.operation_label = None;
                match result {
                    Ok(output) => {
                        let message = if output.is_empty() { "Stint command completed".to_string() } else { output };
                        this.operation_message = Some((true, truncate(&message, 600)));
                    }
                    Err(error) => this.operation_message = Some((false, error)),
                }
                cx.notify();
            });
        })
        .detach();
    }

    fn cancel_pending_action(&mut self, cx: &mut Context<Self>) {
        self.pending_action = None;
        cx.notify();
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

    fn launch_step(&self) -> u8 {
        if self.mission_path.is_empty() || self.repo_path.is_empty() {
            0
        } else if self.mission_preview.is_none() {
            1
        } else if self.pending_run.is_some() {
            2
        } else {
            3
        }
    }

    fn step_indicator(&self, index: u8, label: &str) -> impl IntoElement {
        let current = self.launch_step();
        let (circle, text) = if current < index {
            (rgb(0x3a4150), rgb(0x7d8698))
        } else if current == index {
            (rgb(0x70a4e5), rgb(0xdbe6f5))
        } else {
            (rgb(0x3d6a52), rgb(0x8fd0aa))
        };
        let mark = if current > index { "✓".to_string() } else { index.to_string() };
        h_flex()
            .items_center()
            .gap_2()
            .child(
                div()
                    .w_6()
                    .h_6()
                    .rounded_full()
                    .flex_shrink_0()
                    .border_1()
                    .border_color(circle)
                    .text_sm()
                    .text_color(circle)
                    .child(mark),
            )
            .child(div().text_sm().text_color(text).child(label.to_string()))
    }

    fn progress_bar(&self, verified: usize, total: usize) -> impl IntoElement {
        let fraction = if total > 0 {
            (verified as f64 / total as f64).clamp(0.0, 1.0)
        } else {
            0.0
        };
        h_flex()
            .items_center()
            .w_full()
            .gap_3()
            .child(
                div()
                    .flex_1()
                    .h_2()
                    .rounded_full()
                    .overflow_hidden()
                    .bg(rgb(0x232936))
                    .child(
                        div()
                            .h_full()
                            .rounded_full()
                            .bg(rgb(0x67c587))
                            .w(relative(fraction as f32)),
                    ),
            )
            .child(
                div()
                    .text_sm()
                    .text_color(rgb(0x9ba6b8))
                    .child(format!("{verified}/{total} verified")),
            )
    }

    fn deep_run_panel(&mut self, cx: &mut Context<Self>) -> impl IntoElement {
        let mut panel = v_flex()
            .id("deep-run-content")
            .overflow_y_scroll()
            .w_full()
            .flex_1()
            .min_h_0()
            .gap_4()
            .child(
                h_flex()
                    .items_center()
                    .child(
                        div()
                            .text_xl()
                            .font_weight(FontWeight::BOLD)
                            .child("Create and monitor Deep Work"),
                    )
                    .child(
                        div()
                            .ml_auto()
                            .text_sm()
                            .text_color(if self.run_busy { rgb(0xe6ad69) } else { rgb(0x8993a4) })
                            .child(if self.run_busy {
                                "Working…".to_string()
                            } else {
                                "Refreshes every 5s".to_string()
                            }),
                    ),
            );

        let launch = self.launch_flow(cx);
        panel = panel.child(launch);
        panel.child(self.run_monitor(cx))
    }

    fn launch_flow(&mut self, cx: &mut Context<Self>) -> impl IntoElement {
        let mut card = v_flex()
            .w_full()
            .flex_shrink_0()
            .gap_4()
            .p_4()
            .border_1()
            .border_color(rgb(0x2d333e))
            .rounded_lg()
            .bg(rgb(0x191d24))
            .child(
                h_flex()
                    .items_center()
                    .gap_5()
                    .child(self.step_indicator(0, "Mission & repo"))
                    .child(self.step_indicator(1, "Validate"))
                    .child(self.step_indicator(2, "Compute & launch")),
            );

        let mut step = v_flex().gap_3().p_3().rounded_md().bg(rgb(0x20252e));
        step = step.child(
            h_flex()
                .items_center()
                .gap_2()
                .child(
                    div()
                        .flex_1()
                        .min_w_0()
                        .text_sm()
                        .text_color(rgb(0x9ba6b8))
                        .child(if self.mission_path.is_empty() {
                            "Choose a mission Markdown file".to_string()
                        } else {
                            self.mission_path.clone()
                        }),
                )
                .child(action_button!(
                    "Choose mission",
                    cx.listener(|this, _, _, cx| this.pick_path(true, cx))
                )),
        );
        step = step.child(
            h_flex()
                .items_center()
                .gap_2()
                .child(
                    div()
                        .flex_1()
                        .min_w_0()
                        .text_sm()
                        .text_color(rgb(0x9ba6b8))
                        .child(if self.repo_path.is_empty() {
                            "Choose a clean target repository".to_string()
                        } else {
                            self.repo_path.clone()
                        }),
                )
                .child(action_button!(
                    "Choose repository",
                    cx.listener(|this, _, _, cx| this.pick_path(false, cx))
                )),
        );
        step = step.child(
            h_flex()
                .items_center()
                .gap_2()
                .child(div().text_sm().text_color(rgb(0x8993a4)).child("If new compute is needed"))
                .children([1.0_f64, 2.0, 4.0].into_iter().map(|hours| {
                    let selected = (self.run_hours - hours).abs() < 0.001;
                    div()
                        .id(format!("deep-hours-{hours:.0}"))
                        .px_3()
                        .py_2()
                        .rounded_md()
                        .cursor_pointer()
                        .bg(if selected { rgb(0x354256) } else { rgb(0x20252e) })
                        .text_sm()
                        .on_click(cx.listener(move |this, _, _, cx| {
                            this.run_hours = hours;
                            cx.notify();
                        }))
                        .child(format!("{hours:.0}h"))
                }))
                .child(action_button!(
                    "Validate and preview",
                    cx.listener(|this, _, _, cx| this.preview_mission(cx))
                )),
        );
        card = card.child(step);

        if let Some(preview) = &self.mission_preview {
            card = card.child(
                v_flex()
                    .gap_1()
                    .p_3()
                    .rounded_md()
                    .bg(rgb(0x20252e))
                    .child(
                        div()
                            .font_weight(FontWeight::BOLD)
                            .child(format!("{} · {} tasks", preview.name, preview.tasks.len())),
                    )
                    .child(div().text_sm().child(preview.objective.clone()))
                    .children(
                        preview
                            .tasks
                            .iter()
                            .map(|task| div().text_sm().text_color(rgb(0x9ba6b8)).child(format!("{} · {}", task.id, task.objective))),
                    )
                    .when(!preview.success.is_empty(), |card| {
                        card.child(div().text_sm().child(format!("Success: {}", preview.success.join(" · "))))
                    })
                    .when(!preview.constraints.is_empty(), |card| {
                        card.child(div().text_sm().child(format!("Constraints: {}", preview.constraints.join(" · "))))
                    })
                    .child(div().text_sm().text_color(rgb(0x8993a4)).child(truncate(&preview.content, 5000))),
            );
            card = card.child(action_button!(
                "Review and start",
                cx.listener(|this, _, _, cx| this.request_deep_start(cx))
            ));
        }

        if let Some(review) = &self.pending_run {
            let provision = review.selected.offer_id != "existing-ready-session";
            card = card.child(
                v_flex()
                    .gap_2()
                    .p_3()
                    .rounded_md()
                    .bg(rgb(0x252a34))
                    .child(
                        div()
                            .font_weight(FontWeight::BOLD)
                            .child(if provision {
                                "Confirm compute rental"
                            } else {
                                "Confirm Deep Work start"
                            }),
                    )
                    .child(
                        div().text_sm().child(if provision {
                            format!(
                                "{} · {} · {}h · ${:.2}/hour · estimated ${:.2}",
                                review.selected.gpu_model,
                                review.selected.runtime,
                                review.hours,
                                review.selected.hourly_usd,
                                review.selected.estimated_session_usd
                            )
                        } else {
                            "Reuse the existing READY compute session. No new rental will be requested.".into()
                        }),
                    )
                    .child(
                        h_flex()
                            .gap_2()
                            .child(action_button!(
                                "Confirm",
                                cx.listener(|this, _, _, cx| this.confirm_deep_start(cx))
                            ))
                            .child(action_button!(
                                "Cancel",
                                cx.listener(|this, _, _, cx| {
                                    this.pending_run = None;
                                    cx.notify();
                                })
                            )),
                    ),
            );
        }

        if let Some((success, message)) = &self.run_message {
            card = card.child(
                div()
                    .p_2()
                    .rounded_md()
                    .bg(if *success { rgb(0x1d3429) } else { rgb(0x3a2927) })
                    .text_sm()
                    .text_color(if *success { rgb(0x67c587) } else { rgb(0xe58c8c) })
                    .child(message.clone()),
            );
        }
        card
    }

    fn run_monitor(&mut self, cx: &mut Context<Self>) -> impl IntoElement {
        let mut container = h_flex()
            .id("run-monitor")
            .w_full()
            .flex_1()
            .min_h_0()
            .gap_4();

        let list = if self.run_history.is_empty() && self.run_history_error.is_none() {
            v_flex()
                .w(px(300.))
                .flex_shrink_0()
                .h_full()
                .p_4()
                .border_1()
                .border_color(rgb(0x2d333e))
                .rounded_lg()
                .bg(rgb(0x191d24))
                .child(div().text_sm().text_color(rgb(0x8993a4)).child("No Deep Work runs yet. Launch one above."))
                .into_any_element()
        } else if let Some(error) = &self.run_history_error {
            v_flex()
                .w(px(300.))
                .flex_shrink_0()
                .h_full()
                .p_4()
                .border_1()
                .border_color(rgb(0x2d333e))
                .rounded_lg()
                .bg(rgb(0x191d24))
                .child(div().font_weight(FontWeight::BOLD).child("History"))
                .child(
                    div()
                        .text_sm()
                        .text_color(rgb(0xe6ad69))
                        .child(format!("History unavailable: {error}")),
                )
                .into_any_element()
        } else {
            v_flex()
                .w(px(300.))
                .flex_shrink_0()
                .h_full()
                .p_3()
                .border_1()
                .border_color(rgb(0x2d333e))
                .rounded_lg()
                .bg(rgb(0x191d24))
                .child(div().font_weight(FontWeight::BOLD).child("Runs"))
                .child(
                    div()
                        .id("run-history-list")
                        .overflow_y_scroll()
                        .flex_1()
                        .min_h_0()
                        .children(
                            self.run_history
                                .iter()
                                .map(|record| self.run_history_row(record, cx)),
                        ),
                )
                .into_any_element()
        };
        container = container.child(list);

        let detail = if let Some(record) = self
            .run_history
            .iter()
            .find(|r| Some(r.state.session_id.as_str()) == self.selected_run.as_deref())
        {
            self.run_detail(record, cx).into_any_element()
        } else {
            v_flex()
                .flex_1()
                .min_w_0()
                .h_full()
                .p_4()
                .border_1()
                .border_color(rgb(0x2d333e))
                .rounded_lg()
                .bg(rgb(0x191d24))
                .child(div().text_sm().text_color(rgb(0x8993a4)).child("Select a run to inspect its progress."))
                .into_any_element()
        };
        container.child(detail)
    }

    fn run_history_row(&self, record: &RunRecord, cx: &mut Context<Self>) -> impl IntoElement {
        let id = record.state.session_id.clone();
        let selected = self.selected_run.as_deref() == Some(id.as_str());
        let verified = record.state.tasks.iter().filter(|t| t.status == "verified").count();
        h_flex()
            .id(format!("run-history-{id}"))
            .items_center()
            .gap_2()
            .p_3()
            .mb_1()
            .rounded_md()
            .cursor_pointer()
            .bg(if selected { rgb(0x2b3543) } else { rgb(0x20252e) })
            .on_click(cx.listener(move |this, _, _, cx| {
                this.selected_run = Some(id.clone());
                cx.notify();
            }))
            .child(
                v_flex()
                    .flex_1()
                    .min_w_0()
                    .gap_1()
                    .child(
                        h_flex()
                            .items_center()
                            .gap_1()
                            .child(
                                div()
                                    .flex_1()
                                    .min_w_0()
                                    .text_sm()
                                    .child(truncate(&record.state.mission_name, 28)),
                            )
                            .when(record.latest, |row| {
                                row.child(
                                    div()
                                        .flex_shrink_0()
                                        .px_2()
                                        .py_1()
                                        .rounded_md()
                                        .bg(rgb(0x1d3429))
                                        .text_sm()
                                        .text_color(rgb(0x67c587))
                                        .child("latest"),
                                )
                            }),
                    )
                    .child(
                        div()
                            .text_sm()
                            .text_color(rgb(0x8993a4))
                            .child(format!("{} verified of {}", verified, record.state.tasks.len())),
                    ),
            )
            .child(phase_badge(&record.state.phase))
    }

    fn run_detail(&self, record: &RunRecord, cx: &mut Context<Self>) -> impl IntoElement {
        let terminal = record.state.phase == "landed" || record.state.phase == "stopped";
        let verified = record.state.tasks.iter().filter(|t| t.status == "verified").count();
        let mut card = v_flex()
            .flex_1()
            .min_w_0()
            .h_full()
            .gap_3()
            .p_4()
            .border_1()
            .border_color(rgb(0x2d333e))
            .rounded_lg()
            .bg(rgb(0x191d24))
            .child(
                h_flex()
                    .items_center()
                    .gap_2()
                    .child(
                        div()
                            .flex_1()
                            .min_w_0()
                            .text_base()
                            .font_weight(FontWeight::BOLD)
                            .child(truncate(&record.state.mission_name, 40)),
                    )
                    .child(phase_badge(&record.state.phase)),
            )
            .child(
                div()
                    .text_sm()
                    .text_color(rgb(0x9ba6b8))
                    .child(format!(
                        "Session {} · deadline {} · coordinator {}",
                        record.state.session_id,
                        record.state.deadline.as_deref().unwrap_or("unknown"),
                        if record.coordinator_alive { "running" } else { "not running" }
                    )),
            );

        card = card.child(self.progress_bar(verified, record.state.tasks.len()));

        if record.latest && !terminal {
            card = card.child(
                h_flex()
                    .gap_2()
                    .child(action_button!(
                        "Stop run",
                        cx.listener(|this, _, _, cx| {
                            this.confirm_stop = true;
                            cx.notify();
                        })
                    ))
                    .when(!record.coordinator_alive, |row| {
                        let id = record.state.session_id.clone();
                        row.child(action_button!(
                            "Resume",
                            cx.listener(move |this, _, _, cx| {
                                this.confirm_resume = Some(id.clone());
                                cx.notify();
                            })
                        ))
                    }),
            );
        } else if !record.latest {
            card = card.child(
                div()
                    .text_sm()
                    .text_color(rgb(0x8993a4))
                    .child("Historical run — read only. Controls act on the latest run only."),
            );
        }

        if self.confirm_stop && record.latest {
            card = card.child(
                h_flex()
                    .items_center()
                    .gap_2()
                    .child(div().text_sm().child("Stop this Deep Work run and land its current state?"))
                    .child(action_button!(
                        "Confirm stop",
                        cx.listener(|this, _, _, cx| this.run_lifecycle_action(None, cx))
                    ))
                    .child(action_button!(
                        "Cancel",
                        cx.listener(|this, _, _, cx| {
                            this.confirm_stop = false;
                            cx.notify();
                        })
                    )),
            );
        }
        if let Some(id) = &self.confirm_resume {
            let selected_id = id.clone();
            card = card.child(
                h_flex()
                    .items_center()
                    .gap_2()
                    .child(div().text_sm().child(format!("Resume {selected_id}?")))
                    .child(action_button!(
                        "Confirm resume",
                        cx.listener(move |this, _, _, cx| {
                            this.run_lifecycle_action(Some(selected_id.clone()), cx)
                        })
                    ))
                    .child(action_button!(
                        "Cancel",
                        cx.listener(|this, _, _, cx| {
                            this.confirm_resume = None;
                            cx.notify();
                        })
                    )),
            );
        }

        let tasks = record.state.tasks.clone();
        let incidents = record.incidents.clone();
        let body = v_flex()
            .id("run-detail-body")
            .flex_1()
            .min_h_0()
            .overflow_y_scroll()
            .gap_1()
            .child(div().text_sm().font_weight(FontWeight::BOLD).child("Tasks"))
            .children(
                tasks.iter().map(|task| {
                    h_flex()
                        .items_start()
                        .gap_2()
                        .child(
                            div()
                                .w_16()
                                .flex_shrink_0()
                                .text_sm()
                                .text_color(rgb(0x9ba6b8))
                                .truncate()
                                .child(task.id.clone()),
                        )
                        .child(
                            h_flex()
                                .items_start()
                                .gap_2()
                                .child(
                                    div()
                                        .flex_1()
                                        .min_w_0()
                                        .text_sm()
                                        .child(truncate(&task.objective, 90)),
                                )
                                .when(!task.blocker.is_empty(), |row| {
                                    row.child(
                                        div()
                                            .flex_shrink_0()
                                            .px_2()
                                            .py_1()
                                            .rounded_md()
                                            .bg(rgb(0x3a2927))
                                            .text_sm()
                                            .text_color(rgb(0xe6ad69))
                                            .child(truncate(&task.blocker, 40)),
                                    )
                                }),
                        )
                        .child(phase_badge(&task.status))
                }),
            )
            .when(!incidents.is_empty(), |body| {
                body.child(div().pt_2().text_sm().font_weight(FontWeight::BOLD).child("Recent incidents")).children(
                    incidents.iter().rev().take(6).map(|incident| {
                        div()
                            .text_sm()
                            .text_color(rgb(0x8993a4))
                            .child(format!("{} · {} {} {}", incident.time, incident.kind, incident.task, incident.detail))
                    }),
                )
            });
        card.child(body)
    }

    fn interactive_panel(&self, cx: &mut Context<Self>) -> impl IntoElement {
        let mut panel = v_flex()
            .id("interactive-content")
            .overflow_y_scroll()
            .w_full()
            .flex_1()
            .gap_4()
            .child(
                h_flex()
                    .items_center()
                    .child(
                        div()
                            .text_xl()
                            .font_weight(FontWeight::BOLD)
                            .child("Interactive session"),
                    )
                    .child(
                        div()
                            .ml_auto()
                            .text_sm()
                            .text_color(rgb(0x8993a4))
                            .child(if self.operation_busy {
                                self.operation_label.clone().unwrap_or_else(|| "Working…".to_string())
                            } else {
                                "Status refreshes every 3s · passive telemetry every 12s".to_string()
                            }),
                    ),
            );

        if let Some((success, message)) = &self.operation_message {
            let color = if *success { rgb(0x67c587) } else { rgb(0xe58c8c) };
            panel = panel.child(
                div()
                    .w_full()
                    .p_3()
                    .rounded_md()
                    .bg(color.opacity(0.12))
                    .text_sm()
                    .text_color(color)
                    .child(message.clone()),
            );
        }

        if let Some(snapshot) = self.interactive_status.as_ref().filter(|snapshot| snapshot.active) {
            panel = panel.child(self.interactive_session_card(snapshot));
            panel = panel.child(self.interactive_controls(snapshot, cx));
        } else {
            panel = panel.child(self.interactive_empty_state(cx));
        }

        if let Some(error) = &self.interactive_error {
            panel = panel.child(
                div()
                    .w_full()
                    .p_3()
                    .rounded_md()
                    .bg(rgb(0x3a2927))
                    .text_sm()
                    .text_color(rgb(0xe6ad69))
                    .child(format!("Status unavailable: {error}")),
            );
        }

        if let Some(pending) = &self.pending_action {
            panel = panel.child(self.pending_confirmation(pending, cx));
        }

        panel
    }

    fn interactive_empty_state(&self, cx: &mut Context<Self>) -> impl IntoElement {
        let mut card = v_flex()
            .w_full()
            .gap_4()
            .p_5()
            .border_1()
            .border_color(rgb(0x2d333e))
            .rounded_lg()
            .bg(rgb(0x191d24))
            .child(
                div()
                    .text_base()
                    .font_weight(FontWeight::BOLD)
                    .child("No active interactive session"),
            )
            .child(
                div()
                    .text_sm()
                    .text_color(rgb(0x9ba6b8))
                    .child("Review live GPU offers before renting. Stint will enforce the interactive profile’s hourly and total cost ceilings."),
            )
            .child(
                h_flex()
                    .items_center()
                    .gap_2()
                    .child(div().text_sm().text_color(rgb(0x8993a4)).child("Duration"))
                    .children([1.0_f64, 2.0, 4.0].into_iter().map(|hours| {
                        let selected = (self.selected_hours - hours).abs() < 0.001;
                        div()
                            .id(format!("start-duration-{hours:.0}"))
                            .px_3()
                            .py_2()
                            .rounded_md()
                            .cursor_pointer()
                            .bg(if selected { rgb(0x354256) } else { rgb(0x20252e) })
                            .text_sm()
                            .on_click(cx.listener(move |this, _, _, cx| {
                                this.selected_hours = hours;
                                this.pending_action = None;
                                cx.notify();
                            }))
                            .child(format!("{hours:.0}h"))
                    }))
                    .child(
                        div()
                            .id("start-review-offers")
                            .ml_auto()
                            .px_4()
                            .py_2()
                            .rounded_md()
                            .cursor_pointer()
                            .bg(rgb(0x35624e))
                            .font_weight(FontWeight::BOLD)
                            .text_sm()
                            .on_click(cx.listener(|this, _, _, cx| this.request_start_preview(cx)))
                            .child("Review offers"),
                    ),
            );

        if self.interactive_status.is_none() && self.interactive_error.is_none() {
            card = card.child(
                div()
                    .text_sm()
                    .text_color(rgb(0x8993a4))
                    .child("Checking local Stint session state…"),
            );
        }
        card
    }

    fn interactive_session_card(&self, snapshot: &Snapshot) -> impl IntoElement {
        let status_color = match snapshot.session.status.as_str() {
            "READY" => rgb(0x67c587),
            "RECOVERABLE" => rgb(0xe6ad69),
            "RENTING" | "BOOTING" | "SSH_CONNECTING" | "SSH_READY" | "RUNTIME_BOOTSTRAP"
            | "RUNTIME_READY" | "MODEL_STARTING" | "MODEL_STARTED" | "MODEL_LOADING" => {
                rgb(0x70a4e5)
            }
            _ => rgb(0x8993a4),
        };
        let remaining = format_duration(snapshot.time.remaining_seconds);
        let status_health = if snapshot.session.status == "READY" && snapshot.health.endpoint.refreshed {
            if snapshot.health.endpoint.healthy {
                format!("Healthy · {:.0} ms", snapshot.health.endpoint.latency_milliseconds)
            } else if !snapshot.health.endpoint.meta.error.is_empty() {
                format!("Degraded · {}", truncate(&snapshot.health.endpoint.meta.error, 90))
            } else {
                "Endpoint degraded".to_string()
            }
        } else if snapshot.session.status == "READY" {
            "Health not checked yet".to_string()
        } else {
            "Waiting for READY".to_string()
        };
        let mut card = v_flex()
            .w_full()
            .gap_4()
            .p_5()
            .border_1()
            .border_color(rgb(0x2d333e))
            .rounded_lg()
            .bg(rgb(0x191d24))
            .child(
                h_flex()
                    .items_center()
                    .gap_3()
                    .child(
                        div()
                            .text_base()
                            .font_weight(FontWeight::BOLD)
                            .child(format!("Instance {}", snapshot.session.instance_id)),
                    )
                    .child(status_pill(&snapshot.session.status, status_color))
                    .child(
                        div()
                            .ml_auto()
                            .text_sm()
                            .text_color(if status_health.starts_with("Healthy") { rgb(0x67c587) } else { rgb(0xe6ad69) })
                            .child(status_health),
                    ),
            )
            .child(
                h_flex()
                    .gap_5()
                    .child(metric("GPU", present(&snapshot.session.gpu_model)))
                    .child(metric("Runtime", present(&snapshot.session.runtime)))
                    .child(metric("Model", present(&snapshot.session.model)))
                    .child(metric("Context", format!("{} tokens", snapshot.session.context_tokens))),
            )
            .child(
                h_flex()
                    .gap_5()
                    .child(metric("Remaining", remaining))
                    .child(metric("Hourly", format!("${:.3}/hr", snapshot.cost.hourly_usd)))
                    .child(metric("Spent", format!("${:.2}", snapshot.cost.estimated_spent_usd)))
                    .child(metric("Scheduled", format!("${:.2}", snapshot.cost.scheduled_usd))),
            )
            .child(detail_field("Deadline", present(&snapshot.time.deadline)))
            .child(detail_field("Local endpoint", "http://127.0.0.1:8409/v1".to_string()));

        card = card.child(detail_field(
            "Lifecycle services",
            format!(
                "Tunnel {} · watchdog {} · SSH {} · remote runtime {}",
                health_label(snapshot.health.tunnel.running),
                health_label(snapshot.health.watchdog.running),
                health_label(snapshot.health.runtime.ssh),
                if snapshot.health.runtime.refreshed {
                    if snapshot.health.runtime.running { "running" } else { "not detected" }
                } else {
                    "not checked"
                },
            ),
        ));
        if snapshot.health.runtime.refreshed && !snapshot.health.runtime.meta.error.is_empty() {
            card = card.child(detail_field("Runtime check", snapshot.health.runtime.meta.error.clone()));
        }
        if !snapshot.collected_at.is_empty() {
            card = card.child(detail_field("Snapshot updated", snapshot.collected_at.clone()));
        }

        if !snapshot.session.checkpoint.is_empty() {
            card = card.child(detail_field("Checkpoint", snapshot.session.checkpoint.clone()));
        }
        if !snapshot.session.last_error.is_empty() {
            card = card.child(
                div()
                    .p_3()
                    .rounded_md()
                    .bg(rgb(0x3a2927))
                    .text_sm()
                    .text_color(rgb(0xe6ad69))
                    .child(format!("Startup issue: {}", snapshot.session.last_error)),
            );
        }
        if snapshot.staleness.deadline_stale && !snapshot.staleness.warning.is_empty() {
            card = card.child(
                div()
                    .text_sm()
                    .text_color(rgb(0xe6ad69))
                    .child(snapshot.staleness.warning.clone()),
            );
        }
        if snapshot.session.status == "READY" {
            let traffic = if snapshot.inference.available {
                format!(
                    "{} processing · {} lanes · {:.1} decode tok/s · {:.1} prefill tok/s",
                    snapshot.inference.processing,
                    snapshot.inference.agents,
                    snapshot.inference.decode_tokens_sec,
                    snapshot.inference.prefill_tokens_sec,
                )
            } else if !snapshot.inference.unavailable_reason.is_empty() {
                snapshot.inference.unavailable_reason.clone()
            } else {
                "Live traffic has not been sampled yet".to_string()
            };
            card = card.child(detail_field("Live inference (passive)", traffic));
        }
        card
    }

    fn interactive_controls(&self, snapshot: &Snapshot, cx: &mut Context<Self>) -> impl IntoElement {
        let mut row = h_flex().w_full().items_center().gap_2();
        if snapshot.session.status == "RECOVERABLE" && !snapshot.time.expired && !self.operation_busy {
            let instance_id = snapshot.session.instance_id;
            row = row.child(control_button("Resume existing instance", "#35624e", cx.listener(move |this, _, _, cx| {
                this.pending_action = Some(PendingAction::Resume(instance_id));
                cx.notify();
            })));
        }
        if snapshot.session.status == "READY" && !snapshot.time.expired && !self.operation_busy {
            row = row
                .child(control_button("Extend 30m", "#34445e", cx.listener(|this, _, _, cx| this.request_deadline_preview("extend", "30m", cx))))
                .child(control_button("Shorten 15m", "#4b3e39", cx.listener(|this, _, _, cx| this.request_deadline_preview("shorten", "15m", cx))));
        }
        if snapshot.session.status != "STOPPED" && !self.operation_busy {
            let instance_id = snapshot.session.instance_id;
            row = row.child(control_button("End session…", "#633d3b", cx.listener(move |this, _, _, cx| {
                this.pending_action = Some(PendingAction::Destroy(instance_id));
                cx.notify();
            })));
        }
        if self.operation_busy {
            row = row.child(
                div()
                    .text_sm()
                    .text_color(rgb(0x8993a4))
                    .child(self.operation_label.clone().unwrap_or_else(|| "Working…".to_string())),
            );
        }
        row
    }

    fn pending_confirmation(&self, pending: &PendingAction, cx: &mut Context<Self>) -> impl IntoElement {
        let mut card = v_flex()
            .w_full()
            .gap_3()
            .p_4()
            .border_1()
            .border_color(rgb(0x7c684b))
            .rounded_lg()
            .bg(rgb(0x27231e));
        match pending {
            PendingAction::Start(preview) => {
                card = card
                    .child(div().font_weight(FontWeight::BOLD).child("Confirm GPU rental"))
                    .child(detail_field("Selected offer", format!("{} · {}", preview.selected.gpu_model, preview.selected.location)))
                    .child(detail_field("Price and duration", format!("${:.3}/hr · {:.0}h · estimated ${:.2}", preview.selected.hourly_usd, preview.hours, preview.selected.estimated_session_usd)))
                    .child(detail_field("Runtime", format!("{} · {} context tokens", preview.selected.runtime, preview.selected.context_tokens)))
                    .child(detail_field("Maximum session cost", format!("${:.2}", preview.session_max_cost_usd)))
                    .child(detail_field("Offer ceiling and retries", format!("${:.3}/hr · up to {} reviewed offer(s)", preview.max_hourly_usd, preview.fallback_attempts)))
                    .child(detail_field("Model", preview.model.clone()))
                    .child(detail_field("Endpoint", preview.endpoint.clone()));
                let fallbacks: Vec<String> = preview
                    .candidates
                    .iter()
                    .skip(1)
                    .map(|candidate| format!("{} · {} · ${:.3}/hr · ${:.2} total{}", candidate.gpu_model, candidate.location, candidate.hourly_usd, candidate.estimated_session_usd, if candidate.within_cost_ceiling { "" } else { " · over cap, will be skipped" }))
                    .collect();
                if !fallbacks.is_empty() {
                    card = card.child(detail_field("Possible retries", fallbacks.join("; ")));
                }
                card = card.child(div().text_sm().text_color(rgb(0xe6ad69)).child("Confirming allows Stint to rent the selected offer and try listed eligible fallbacks. Startup failures after SSH may preserve a paid instance for Resume."));
            }
            PendingAction::Deadline { duration, preview } => {
                card = card
                    .child(div().font_weight(FontWeight::BOLD).child(format!("Confirm {} session", preview.direction)))
                    .child(detail_field("Current remaining", format_duration(preview.current_remaining_seconds)))
                    .child(detail_field("Change", format!("{} {}", if preview.direction == "extend" { "+" } else { "−" }, duration)))
                    .child(detail_field("Current deadline", preview.previous_deadline.clone()))
                    .child(detail_field("New deadline", preview.new_deadline.clone()))
                    .child(detail_field("Projected session cost", format!("${:.2} / ${:.2} ceiling", preview.projected_usd, preview.session_ceiling_usd)))
                    .child(detail_field("Scheduled cost before change", format!("${:.2}", preview.current_scheduled_usd)))
                    .child(detail_field("Deadline adjustment", format_duration(preview.delta_seconds)))
                    .child(detail_field("Cost change", format!("${:+.2}", preview.exposure_delta_usd)));
            }
            PendingAction::Resume(instance_id) => {
                card = card
                    .child(div().font_weight(FontWeight::BOLD).child("Resume saved session?"))
                    .child(div().text_sm().child(format!("This reattaches to instance {instance_id}; it will not rent replacement compute.")));
            }
            PendingAction::Destroy(instance_id) => {
                card = card
                    .child(div().font_weight(FontWeight::BOLD).text_color(rgb(0xffb2a8)).child("End interactive session?"))
                    .child(div().text_sm().child(format!("This immediately destroys Vast instance {instance_id}.")));
            }
        }
        card.child(
            h_flex()
                .gap_2()
                .child(control_button("Confirm", "#35624e", cx.listener(|this, _, _, cx| this.confirm_pending_action(cx))))
                .child(control_button("Cancel", "#303640", cx.listener(|this, _, _, cx| this.cancel_pending_action(cx)))),
        )
    }

    fn interactive_dash_panel(&self) -> impl IntoElement {
        let mut panel = v_flex()
            .id("interactive-dash-content")
            .overflow_y_scroll()
            .w_full()
            .flex_1()
            .gap_4()
            .child(
                h_flex()
                    .items_center()
                    .child(
                        div()
                            .text_xl()
                            .font_weight(FontWeight::BOLD)
                            .child("Interactive dashboard"),
                    )
                    .child(
                        div()
                            .ml_auto()
                            .text_sm()
                            .text_color(rgb(0x8993a4))
                            .child("Live compute · GPU · inference · refreshes every 5s"),
                    ),
            );

        if let Some(error) = &self.interactive_dash_error {
            panel = panel.child(
                div()
                    .w_full()
                    .p_3()
                    .rounded_md()
                    .bg(rgb(0x3a2927))
                    .text_sm()
                    .text_color(rgb(0xe6ad69))
                    .child(format!("Dashboard unavailable: {error}")),
            );
        }

        let Some(model) = self.interactive_dash.as_ref() else {
            panel = panel.child(
                v_flex()
                    .w_full()
                    .p_5()
                    .border_1()
                    .border_color(rgb(0x2d333e))
                    .rounded_lg()
                    .bg(rgb(0x191d24))
                    .child(div().text_sm().text_color(rgb(0x8993a4)).child("Loading dashboard…")),
            );
            return panel;
        };

        if !model.error.is_empty() {
            panel = panel.child(
                div()
                    .w_full()
                    .p_3()
                    .rounded_md()
                    .bg(rgb(0x3a2927))
                    .text_sm()
                    .text_color(rgb(0xe6ad69))
                    .child(model.error.clone()),
            );
        }

        // Session header card.
        let status = model.session.status.clone();
        let status_color = match status.as_str() {
            "READY" => rgb(0x67c587),
            "RECOVERABLE" => rgb(0xe6ad69),
            "" => rgb(0x8993a4),
            _ => rgb(0x70a4e5),
        };
        let header = v_flex()
            .w_full()
            .gap_3()
            .p_4()
            .border_1()
            .border_color(rgb(0x2d333e))
            .rounded_lg()
            .bg(rgb(0x191d24))
            .child(
                h_flex()
                    .items_center()
                    .gap_3()
                    .child(
                        div()
                            .text_base()
                            .font_weight(FontWeight::BOLD)
                            .child(if model.no_session || status.is_empty() {
                                "No compute session".to_string()
                            } else {
                                format!("Instance #{} · {}", model.session.instance_id, model.session.gpu_model)
                            }),
                    )
                    .when(!status.is_empty() && !model.no_session, |h| {
                        h.child(status_pill(&status, status_color))
                    }),
            );
        let mut header = header;
        if !model.no_session && !status.is_empty() {
            header = header
                .child(
                    h_flex()
                        .gap_5()
                        .child(metric("Model", present(&model.session.model)))
                        .child(metric("Runtime", present(&model.session.runtime)))
                        .child(metric("Context", format!("{} tok", model.session.context)))
                        .child(metric("Rate", format!("${:.3}/hr", model.session.rate)))
                        .child(metric("Spent est.", format!("${:.2}", model.session.spent)))
                        .child(metric("Exposure", format!("${:.2}", model.session.exposure))),
                )
                .child(
                    h_flex()
                        .gap_5()
                        .child(metric("Elapsed", format_duration(dashboard::ns_to_secs(model.session.elapsed) as f64)))
                        .child(metric("Remaining", format_duration(dashboard::ns_to_secs(model.session.remaining) as f64)))
                        .child(metric("Scheduled", format_duration(dashboard::ns_to_secs(model.session.scheduled) as f64)))
                        .child(detail_field("Deadline", present(&model.session.deadline))),
                );
        }
        panel = panel.child(header);

        // GPU + health row.
        let gpu = &model.gpu;
        let health = &model.health;
        let gpu_card = v_flex()
            .flex_1()
            .gap_2()
            .p_4()
            .border_1()
            .border_color(rgb(0x2d333e))
            .rounded_lg()
            .bg(rgb(0x191d24))
            .child(
                h_flex()
                    .items_center()
                    .child(div().text_sm().font_weight(FontWeight::BOLD).child("GPU"))
                    .child(
                        div()
                            .ml_auto()
                            .child(status_pill(if gpu.available { "available" } else { "n/a" }, if gpu.available { rgb(0x67c587) } else { rgb(0x8993a4) })),
                    ),
            )
            .child(
                h_flex()
                    .gap_4()
                    .child(metric("Util", present(&gpu.utilization)))
                    .child(metric("VRAM", present(&gpu.vram)))
                    .child(metric("Power", present(&gpu.power)))
                    .child(metric("Temp", present(&gpu.temperature))),
            )
            .when(!gpu.error.is_empty(), |c| c.child(div().text_sm().text_color(rgb(0xe6ad69)).child(gpu.error.clone())));
        let health_card = v_flex()
            .flex_1()
            .gap_2()
            .p_4()
            .border_1()
            .border_color(rgb(0x2d333e))
            .rounded_lg()
            .bg(rgb(0x191d24))
            .child(div().text_sm().font_weight(FontWeight::BOLD).child("Health"))
            .child(detail_field("Endpoint", present(&health.endpoint)))
            .child(detail_field("Tunnel", present(&health.tunnel)))
            .child(detail_field("Runtime", present(&health.runtime)))
            .child(detail_field("Watchdog", present(&health.watchdog)))
            .child(detail_field("SSH", present(&health.ssh)));
        panel = panel.child(h_flex().w_full().gap_4().child(gpu_card).child(health_card));

        // Inference card with lane table.
        let inf = &model.inference;
        let mut inf_card = v_flex()
            .w_full()
            .gap_3()
            .p_4()
            .border_1()
            .border_color(rgb(0x2d333e))
            .rounded_lg()
            .bg(rgb(0x191d24))
            .child(
                h_flex()
                    .items_center()
                    .child(div().text_sm().font_weight(FontWeight::BOLD).child("Inference"))
                    .when(inf.stale, |h| h.child(div().ml_auto().child(status_pill("stale", rgb(0xe6ad69)))))
                    .when(inf.stale && !inf.sample_age.is_empty(), |h| {
                        h.child(div().ml_auto().text_sm().text_color(rgb(0x8993a4)).child(inf.sample_age.clone()))
                    }),
            )
            .child(
                h_flex()
                    .gap_5()
                    .child(metric("Decode", present(&inf.decode)))
                    .child(metric(if inf.prefill_label.is_empty() { "Prefill" } else { &inf.prefill_label }, present(&inf.prefill)))
                    .child(metric("Queue", present(&inf.queue)))
                    .child(metric("Cache reuse", present(&inf.cache_reuse)))
                    .child(metric("Speculative", present(&inf.speculative)))
                    .child(metric("Lanes", present(&inf.lanes))),
            );
        if !inf.lane_rows.is_empty() {
            let lane_row = |cells: Vec<(String, bool)>| {
                h_flex()
                    .w_full()
                    .gap_2()
                    .text_sm()
                    .children(
                        cells
                            .into_iter()
                            .zip([0.12_f32, 0.12, 0.34, 0.21, 0.21].iter().copied())
                            .map(|((text, muted), frac)| {
                                div()
                                    .w(relative(frac))
                                    .min_w_0()
                                    .truncate()
                                    .text_color(if muted { rgb(0x91a0b5) } else { rgb(0x9ba6b8) })
                                    .child(text)
                            }),
                    )
            };
            inf_card = inf_card.child(
                v_flex()
                    .gap_1()
                    .child(lane_row(vec![
                        ("Lane".to_string(), true),
                        ("Depth".to_string(), true),
                        ("Status".to_string(), true),
                        ("Cache".to_string(), true),
                        ("Decode".to_string(), true),
                    ]))
                    .children(inf.lane_rows.iter().map(|lane| {
                        lane_row(vec![
                            (format!("#{}", lane.id), false),
                            (format!("d{}", lane.depth), false),
                            (lane.status.clone(), false),
                            (lane.cache.clone(), false),
                            (lane.decode.clone(), false),
                        ])
                    })),
            );
        }
        panel = panel.child(inf_card);

        // Performance card.
        let perf = &model.perf;
        let perf_card = v_flex()
            .w_full()
            .gap_2()
            .p_4()
            .border_1()
            .border_color(rgb(0x2d333e))
            .rounded_lg()
            .bg(rgb(0x191d24))
            .child(
                h_flex()
                    .items_center()
                    .child(div().text_sm().font_weight(FontWeight::BOLD).child("Performance"))
                    .when(perf.benchmarking, |h| h.child(div().ml_auto().child(status_pill("benchmarking", rgb(0x70a4e5)))))
                    .when(!perf.benchmarking && perf.age.is_empty(), |h| {
                        h.child(div().ml_auto().text_sm().text_color(rgb(0x8993a4)).child("no sample yet"))
                    }),
            )
            .child(
                h_flex()
                    .gap_5()
                    .child(metric("TTFT", present(&perf.ttft)))
                    .child(metric("Total", present(&perf.total)))
                    .child(metric("Decode", present(&perf.decode)))
                    .child(metric("Prompt tok", perf.prompt_tokens.to_string()))
                    .child(metric("Completion tok", perf.completion_tokens.to_string()))
                    .child(metric("Age", present(&perf.age))),
            )
            .when(!perf.error.is_empty(), |c| c.child(div().text_sm().text_color(rgb(0xe6ad69)).child(perf.error.clone())));
        panel = panel.child(perf_card);

        // Logs + lane events.
        if !model.logs.is_empty() || !model.lane_events.is_empty() {
            let mut logs_card = v_flex()
                .w_full()
                .gap_2()
                .p_4()
                .border_1()
                .border_color(rgb(0x2d333e))
                .rounded_lg()
                .bg(rgb(0x111318))
                .child(div().text_sm().font_weight(FontWeight::BOLD).child("Logs & lane events"));
            for line in model.lane_events.iter().chain(model.logs.iter()) {
                logs_card = logs_card.child(div().text_sm().text_color(rgb(0x9ba6b8)).child(line.clone()));
            }
            panel = panel.child(logs_card);
        }

        if !model.notice.is_empty() {
            panel = panel.child(
                div()
                    .w_full()
                    .p_3()
                    .rounded_md()
                    .bg(rgb(0x27231e))
                    .text_sm()
                    .text_color(rgb(0xe6ad69))
                    .child(model.notice.clone()),
            );
        }

        panel
    }

    fn deep_board_panel(&self) -> impl IntoElement {
        let mut panel = v_flex()
            .id("deep-board-content")
            .overflow_y_scroll()
            .w_full()
            .flex_1()
            .gap_4()
            .child(
                h_flex()
                    .items_center()
                    .child(
                        div()
                            .text_xl()
                            .font_weight(FontWeight::BOLD)
                            .child("Deep Work board"),
                    )
                    .child(
                        div()
                            .ml_auto()
                            .text_sm()
                            .text_color(rgb(0x8993a4))
                            .child("Durable run progress · refreshes every 5s"),
                    ),
            );

        if let Some(error) = &self.deep_dash_error {
            panel = panel.child(
                div()
                    .w_full()
                    .p_3()
                    .rounded_md()
                    .bg(rgb(0x3a2927))
                    .text_sm()
                    .text_color(rgb(0xe6ad69))
                    .child(format!("Deep board unavailable: {error}")),
            );
        }

        let Some(model) = self.deep_dash.as_ref() else {
            panel = panel.child(
                v_flex()
                    .w_full()
                    .p_5()
                    .border_1()
                    .border_color(rgb(0x2d333e))
                    .rounded_lg()
                    .bg(rgb(0x191d24))
                    .child(div().text_sm().text_color(rgb(0x8993a4)).child("Loading deep board…")),
            );
            return panel;
        };

        if !model.error.is_empty() {
            panel = panel.child(
                div()
                    .w_full()
                    .p_3()
                    .rounded_md()
                    .bg(rgb(0x3a2927))
                    .text_sm()
                    .text_color(rgb(0xe6ad69))
                    .child(model.error.clone()),
            );
        }

        let empty_run = model.session_id.is_empty() && model.mission.is_empty();
        let verified = model.tasks.iter().filter(|t| t.status == "verified").count();

        // Header card.
        let mut header = v_flex()
            .w_full()
            .gap_3()
            .p_4()
            .border_1()
            .border_color(rgb(0x2d333e))
            .rounded_lg()
            .bg(rgb(0x191d24))
            .child(
                h_flex()
                    .items_center()
                    .gap_3()
                    .child(
                        div()
                            .text_base()
                            .font_weight(FontWeight::BOLD)
                            .child(if empty_run { "No Deep Work run".to_string() } else { present(&model.mission) }),
                    )
                    .child(phase_badge(&model.phase))
                    .when(model.latest && !empty_run, |h| h.child(status_pill("latest", rgb(0x65b7b0))))
                    .when(model.can_land && !empty_run, |h| h.child(status_pill("can land", rgb(0x67c587)))),
            );
        if !empty_run {
            header = header
                .child(
                    h_flex()
                        .gap_5()
                        .child(detail_field("Session", present(&model.session_id)))
                        .child(detail_field("Branch", present(&model.branch)))
                        .child(detail_field("Base commit", present(&model.base_commit)))
                        .child(detail_field("Coordinator", present(&model.coordinator)))
                        .child(detail_field("Worker", present(&model.worker_name))),
                )
                .child(
                    h_flex()
                        .gap_5()
                        .child(detail_field("Started", present(&model.started_at)))
                        .child(detail_field("Deadline", present(&model.deadline)))
                        .child(detail_field("Land before", present(&model.land_before)))
                        .child(detail_field("Verify", present(&model.verify))),
                );
            if !model.landing_reason.is_empty() || !model.landing_commit.is_empty() {
                header = header.child(detail_field("Landing", format!("{} · {} · verify {}{}", model.landing_reason, model.landing_commit, if model.landing_verify_done { "done" } else { "pending" }, if model.landed_at.is_some() { " · landed" } else { "" })));
            }
            if !empty_run {
                header = header.child(self.progress_bar(verified, model.tasks.len()));
            }
        }
        panel = panel.child(header);

        // Compute + worker row.
        let compute = &model.compute;
        let worker = &model.worker;
        let compute_card = v_flex()
            .flex_1()
            .gap_2()
            .p_4()
            .border_1()
            .border_color(rgb(0x2d333e))
            .rounded_lg()
            .bg(rgb(0x191d24))
            .child(
                h_flex()
                    .items_center()
                    .child(div().text_sm().font_weight(FontWeight::BOLD).child("Compute"))
                    .child(
                        div()
                            .ml_auto()
                            .child(status_pill(if compute.available { "available" } else { "n/a" }, if compute.available { rgb(0x67c587) } else { rgb(0x8993a4) })),
                    ),
            )
            .child(detail_field("Status", present(&compute.status)))
            .child(detail_field("Endpoint", present(&compute.endpoint)))
            .child(detail_field("Runtime", present(&compute.runtime)))
            .child(detail_field("GPU", present(&compute.gpu)))
            .child(
                h_flex()
                    .gap_4()
                    .child(metric("Util", present(&compute.utilization)))
                    .child(metric("VRAM", present(&compute.vram)))
                    .child(metric("Lane", present(&compute.lane))),
            )
            .when(!compute.error.is_empty(), |c| c.child(div().text_sm().text_color(rgb(0xe6ad69)).child(compute.error.clone())));
        let worker_card = v_flex()
            .flex_1()
            .gap_2()
            .p_4()
            .border_1()
            .border_color(rgb(0x2d333e))
            .rounded_lg()
            .bg(rgb(0x191d24))
            .child(
                h_flex()
                    .items_center()
                    .child(div().text_sm().font_weight(FontWeight::BOLD).child("Worker"))
                    .child(
                        div()
                            .ml_auto()
                            .child(status_pill(
                                if worker.reachable { "reachable" } else if worker.observed { "observed" } else { "none" },
                                if worker.reachable { rgb(0x67c587) } else if worker.observed { rgb(0xe6ad69) } else { rgb(0x8993a4) },
                            )),
                    ),
            )
            .child(detail_field("Phase", present(&worker.latest_phase)))
            .child(detail_field("At", present(&worker.latest_at)))
            .child(
                h_flex()
                    .gap_4()
                    .child(metric("NInfer ctx", worker.n_infer_context.to_string()))
                    .child(metric("NInfer KV", worker.n_infer_kv.to_string()))
                    .child(metric("xHigh", worker.x_high_requests.to_string()))
                    .child(metric("Medium", worker.medium_requests.to_string())),
            )
            .child(
                h_flex()
                    .gap_4()
                    .child(metric("Compression", present(&worker.compression)))
                    .child(metric("Completed", worker.compression_completed.to_string()))
                    .child(metric("Failed", worker.compression_failed.to_string()))
                    .child(metric("Truncated", worker.truncated.to_string())),
            )
            .when(!worker.error.is_empty(), |c| c.child(div().text_sm().text_color(rgb(0xe6ad69)).child(worker.error.clone())))
            .when(!worker.hermes_log.is_empty(), |c| c.child(detail_field("Hermes log", truncate(&worker.hermes_log, 300))));
        panel = panel.child(h_flex().w_full().gap_4().child(compute_card).child(worker_card));

        // Tasks table.
        if !model.tasks.is_empty() {
            let tasks_card = v_flex()
                .w_full()
                .gap_2()
                .p_4()
                .border_1()
                .border_color(rgb(0x2d333e))
                .rounded_lg()
                .bg(rgb(0x191d24))
                .child(div().text_sm().font_weight(FontWeight::BOLD).child(format!("Tasks ({} verified of {})", verified, model.tasks.len())))
                .children(
                    model.tasks.iter().map(|task| {
                        h_flex()
                            .items_start()
                            .gap_2()
                            .child(div().w_16().flex_shrink_0().text_sm().text_color(rgb(0x9ba6b8)).truncate().child(task.id.clone()))
                            .child(div().flex_1().min_w_0().text_sm().child(truncate(&task.objective, 100)))
                            .when(!task.blocker.is_empty(), |r| r.child(div().flex_shrink_0().px_2().py_1().rounded_md().bg(rgb(0x3a2927)).text_sm().text_color(rgb(0xe6ad69)).child(truncate(&task.blocker, 40))))
                            .child(phase_badge(&task.status))
                    }),
                );
            panel = panel.child(tasks_card);
        }

        // Recent events.
        if !model.events.is_empty() {
            let mut events_card = v_flex()
                .w_full()
                .gap_1()
                .p_4()
                .border_1()
                .border_color(rgb(0x2d333e))
                .rounded_lg()
                .bg(rgb(0x111318))
                .child(div().text_sm().font_weight(FontWeight::BOLD).child("Recent events"));
            for event in model.events.iter().rev().take(10) {
                events_card = events_card.child(div().text_sm().text_color(rgb(0x8993a4)).child(format!("{} · {} {} {}", event.time, event.kind, event.task, event.detail)));
            }
            panel = panel.child(events_card);
        }

        if !model.notice.is_empty() {
            panel = panel.child(
                div()
                    .w_full()
                    .p_3()
                    .rounded_md()
                    .bg(rgb(0x27231e))
                    .text_sm()
                    .text_color(rgb(0xe6ad69))
                    .child(model.notice.clone()),
            );
        }

        panel
    }

    fn session_overview(&self) -> impl IntoElement {
        let mut panel = v_flex()
            .w_full()
            .gap_3()
            .p_5()
            .border_1()
            .border_color(rgb(0x2d333e))
            .rounded_lg()
            .bg(rgb(0x191d24));

        match &self.state {
            Some(state) => {
                panel = panel
                    .child(
                        h_flex()
                            .items_center()
                            .gap_3()
                            .child(
                                div()
                                    .text_base()
                                    .font_weight(FontWeight::BOLD)
                                    .child(state.mission_name.clone()),
                            )
                            .child(phase_badge(&state.phase)),
                    )
                    .child(
                        div()
                            .text_sm()
                            .text_color(rgb(0xb4bbc7))
                            .child(state.objective.clone()),
                    )
                    .child(
                        h_flex()
                            .gap_5()
                            .text_sm()
                            .text_color(rgb(0x8e97a7))
                            .child(format!("Session {}", state.session_id))
                            .child(format!("{} tasks", state.tasks.len()))
                            .child(format!("Branch {}", state.branch))
                            .child(format!("Deadline {}", state.deadline.as_deref().unwrap_or("—"))),
                    );
                if let Some(handoff_path) = &state.handoff_path {
                    panel = panel.child(
                        div()
                            .text_sm()
                            .text_color(rgb(0x8e97a7))
                            .child(format!("Handoff {handoff_path}")),
                    );
                }
                if let Some(error) = &self.refresh_error {
                    panel = panel.child(
                        div()
                            .text_sm()
                            .text_color(rgb(0xe6ad69))
                            .child(format!("Refresh issue: {error}")),
                    );
                }
            }
            None => {
                panel = panel
                    .child(
                        div()
                            .text_base()
                            .font_weight(FontWeight::BOLD)
                            .child("No Deep Work session loaded"),
                    )
                    .child(
                        div()
                            .text_sm()
                            .text_color(rgb(0xe58c8c))
                            .child(
                                self.refresh_error
                                    .clone()
                                    .unwrap_or_else(|| "Waiting for session state".to_string()),
                            ),
                    )
                    .child(
                        div()
                            .text_sm()
                            .text_color(rgb(0x8e97a7))
                            .child("Reads the latest session from ~/.local/state/stint/deep/latest."),
                    );
            }
        }

        panel
    }

    fn work_area(&mut self, cx: &mut Context<Self>) -> impl IntoElement {
        h_flex()
            .w_full()
            .flex_1()
            .gap_4()
            .child(self.task_list(cx))
            .child(self.task_detail())
    }

    fn task_list(&self, cx: &mut Context<Self>) -> impl IntoElement {
        let mut panel = v_flex()
            .w(px(390.))
            .h_full()
            .gap_2()
            .p_3()
            .border_1()
            .border_color(rgb(0x2d333e))
            .rounded_lg()
            .bg(rgb(0x191d24))
            .id("task-list")
            .overflow_y_scroll();

        panel = panel.child(
            h_flex()
                .items_center()
                .pb_2()
                .child(
                    div()
                        .font_weight(FontWeight::BOLD)
                        .child("Tasks"),
                )
                .child(
                    div()
                        .ml_auto()
                        .text_sm()
                        .text_color(rgb(0x8993a4))
                        .child("Select a task for details"),
                ),
        );

        if let Some(state) = &self.state {
            panel = panel.children(state.tasks.iter().map(|task| {
                let task_id = task.id.clone();
                let is_selected = self.selected_task.as_deref() == Some(task.id.as_str());
                h_flex()
                    .id(format!("task-{}", task.id))
                    .items_center()
                    .gap_2()
                    .p_3()
                    .rounded_md()
                    .when(is_selected, |row| row.bg(rgb(0x2b3543)))
                    .when(!is_selected, |row| row.bg(rgb(0x20252e)))
                    .on_click(cx.listener(move |this, _, _, cx| {
                        this.selected_task = Some(task_id.clone());
                        cx.notify();
                    }))
                    .child(
                        div()
                            .w_16()
                            .text_sm()
                            .text_color(rgb(0x9ba6b8))
                            .child(task.id.clone()),
                    )
                    .child(
                        div()
                            .flex_1()
                            .text_sm()
                            .child(truncate(&task.objective, 76)),
                    )
                    .child(phase_badge(&task.status))
            }));
        } else {
            panel = panel.child(
                div()
                    .p_3()
                    .text_sm()
                    .text_color(rgb(0x8993a4))
                    .child("Waiting for task data"),
            );
        }

        panel
    }

    fn task_detail(&self) -> impl IntoElement {
        let mut panel = v_flex()
            .flex_1()
            .h_full()
            .gap_3()
            .p_4()
            .border_1()
            .border_color(rgb(0x2d333e))
            .rounded_lg()
            .bg(rgb(0x191d24))
            .id("task-detail")
            .overflow_y_scroll();

        match self.selected_task() {
            Some(task) => {
                panel = panel
                    .child(
                        h_flex()
                            .items_center()
                            .gap_3()
                            .child(
                                div()
                                    .text_base()
                                    .font_weight(FontWeight::BOLD)
                                    .child(task.id.clone()),
                            )
                            .child(phase_badge(&task.status))
                            .child(
                                div()
                                    .ml_auto()
                                    .text_sm()
                                    .text_color(rgb(0x9ba6b8))
                                    .child(format!("{} attempts", task.attempts)),
                            ),
                    )
                    .child(detail_field("Objective", task.objective.clone()));

                if !task.acceptance.is_empty() {
                    panel = panel.child(detail_field("Acceptance", task.acceptance.clone()));
                }
                let verify = if task.verification_command.is_empty() {
                    task.verify.clone()
                } else {
                    task.verification_command.clone()
                };
                if !verify.is_empty() {
                    panel = panel.child(detail_field("Verification command", verify));
                }
                if !task.verification_result.is_empty() {
                    panel = panel.child(detail_field(
                        "Verification result",
                        task.verification_result.clone(),
                    ));
                }
                if !task.blocker.is_empty() {
                    panel = panel.child(detail_field("Blocker", task.blocker.clone()));
                }
                if !task.last_result.is_empty() {
                    panel = panel.child(detail_field(
                        "Last result",
                        truncate(&task.last_result, 900),
                    ));
                }
                if !task.verification_output.is_empty() {
                    panel = panel.child(detail_field(
                        "Verification output",
                        truncate(&task.verification_output, 900),
                    ));
                }
            }
            None => {
                panel = panel.child(
                    div()
                        .text_sm()
                        .text_color(rgb(0x8993a4))
                        .child("Select a task to inspect its details."),
                );
            }
        }

        panel
    }

    fn selected_task(&self) -> Option<&Task> {
        let state = self.state.as_ref()?;
        let selected = self.selected_task.as_deref()?;
        state.tasks.iter().find(|task| task.id == selected)
    }
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

fn detail_field(label: &str, value: String) -> impl IntoElement {
    v_flex()
        .gap_1()
        .child(
            div()
                .text_sm()
                .text_color(rgb(0x91a0b5))
                .child(label.to_string()),
        )
        .child(div().text_sm().child(value))
}

fn phase_badge(phase: &str) -> impl IntoElement {
    let color = match phase {
        "executing" | "verified" => rgb(0x67c587),
        "active" | "attempting" => rgb(0x70a4e5),
        "landing" | "incomplete" | "needs_human" => rgb(0xe6ad69),
        "landed" => rgb(0x65b7b0),
        "blocked" => rgb(0xe58c8c),
        "stopped" => rgb(0x8993a4),
        _ => rgb(0x8993a4),
    };
    let label = if phase.is_empty() {
        "unknown".to_string()
    } else {
        phase.to_string()
    };
    div()
        .px_2()
        .py_1()
        .rounded_md()
        .bg(color.opacity(0.16))
        .text_color(color)
        .text_sm()
        .child(label)
}

fn status_pill(label: &str, color: Rgba) -> impl IntoElement {
    div()
        .px_2()
        .py_1()
        .rounded_md()
        .bg(color.opacity(0.16))
        .text_color(color)
        .text_sm()
        .child(label.to_string())
}

fn metric(label: &str, value: String) -> impl IntoElement {
    v_flex()
        .flex_1()
        .gap_1()
        .child(div().text_sm().text_color(rgb(0x91a0b5)).child(label.to_string()))
        .child(div().text_sm().child(value))
}

fn control_button(
    label: &str,
    color: &str,
    on_click: impl Fn(&ClickEvent, &mut Window, &mut App) + 'static,
) -> impl IntoElement {
    let color = u32::from_str_radix(color.trim_start_matches('#'), 16).unwrap_or(0x303640);
    div()
        .id(label.to_string())
        .px_4()
        .py_2()
        .rounded_md()
        .cursor_pointer()
        .bg(rgb(color))
        .text_sm()
        .on_click(on_click)
        .child(label.to_string())
}

fn present(value: &str) -> String {
    if value.is_empty() { "—".to_string() } else { value.to_string() }
}

fn health_label(running: bool) -> &'static str {
    if running { "running" } else { "not running" }
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

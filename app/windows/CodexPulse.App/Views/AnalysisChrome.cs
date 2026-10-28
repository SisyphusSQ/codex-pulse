using System.Collections.ObjectModel;
using System.ComponentModel;
using Microsoft.UI.Xaml;
using Microsoft.UI.Xaml.Automation;
using Microsoft.UI.Xaml.Controls;
using Microsoft.UI.Xaml.Controls.Primitives;
using Microsoft.UI.Xaml.Markup;
using CodexPulse.AppSupport;

namespace CodexPulse.App.Views;

public sealed class AnalysisRow : INotifyPropertyChanged
{
    public required string Identity{get;init;}
    public required object Source{get;set;}
    public string Title{get;set;}="";
    public string Subtitle{get;set;}="";
    public string Metrics{get;set;}="";
    public string Evidence{get;set;}="";
    public event PropertyChangedEventHandler? PropertyChanged;
    public void Update(AnalysisRow row){Source=row.Source;Title=row.Title;Subtitle=row.Subtitle;Metrics=row.Metrics;Evidence=row.Evidence;PropertyChanged?.Invoke(this,new(""));}
    public override string ToString()=>Title+" · "+Metrics;
}

internal sealed class AnalysisChrome
{
    public Grid View{get;}=new(){RowSpacing=12,Padding=new Thickness(0,8,0,12)};
    public StackPanel Filters{get;}=new(){Spacing=12};
    public TextBlock Summary{get;}=PresentationControls.Text("");
    public TextBlock Notice{get;}=PresentationControls.Text("");
    public StackPanel Detail{get;}=new(){Spacing=16,Padding=new Thickness(4,4,12,24)};
    public TextBlock DetailStatus{get;}=PresentationControls.Text("");
    public StackPanel DetailBody{get;}=new(){Spacing=16};
    public ListView List{get;}=new(){SelectionMode=ListViewSelectionMode.Single,IsItemClickEnabled=true,HorizontalContentAlignment=HorizontalAlignment.Stretch};
    public Button First{get;}
    public Button Previous{get;}
    public Button Next{get;}
    public Button Back{get;}
    public TextBlock PageLabel{get;}=PresentationControls.Text("");
    private readonly Grid split=new(){ColumnSpacing=8};
    private readonly Grid listPane=new(){RowSpacing=10};
    private readonly ScrollViewer detailPane;
    private readonly Thumb divider=new(){Width=6,Background=Charts.NeutralBrush,HorizontalAlignment=HorizontalAlignment.Stretch,IsTabStop=true};
    private readonly ObservableCollection<AnalysisRow> rows=[];
    private bool updating,selected;
    private double listWidth=410;
    public event Action<AnalysisRow>? Selected;
    public AnalysisChrome(Localization l)
    {
        View.RowDefinitions.Add(new(){Height=GridLength.Auto});View.RowDefinitions.Add(new(){Height=GridLength.Auto});View.RowDefinitions.Add(new(){Height=new GridLength(1,GridUnitType.Star)});
        var filterScroll=new ScrollViewer{Content=Filters,MaxHeight=260,VerticalScrollBarVisibility=ScrollBarVisibility.Auto,HorizontalScrollBarVisibility=ScrollBarVisibility.Disabled,HorizontalContentAlignment=HorizontalAlignment.Stretch};
        var filterExpander=new Expander{Header=l.Text("筛选与排序","Filters and sorting"),Content=filterScroll,HorizontalAlignment=HorizontalAlignment.Stretch,HorizontalContentAlignment=HorizontalAlignment.Stretch};
        View.SizeChanged+=(_,args)=>filterScroll.MaxHeight=Math.Clamp(args.NewSize.Height*.45,120,300);
        View.Children.Add(filterExpander);
        var heading=new StackPanel{Spacing=4};heading.Children.Add(Summary);heading.Children.Add(Notice);Notice.Opacity=.75;Grid.SetRow(heading,1);View.Children.Add(heading);
        split.ColumnDefinitions.Add(new(){Width=new GridLength(listWidth)});split.ColumnDefinitions.Add(new(){Width=GridLength.Auto});split.ColumnDefinitions.Add(new(){Width=new GridLength(1,GridUnitType.Star)});
        listPane.RowDefinitions.Add(new(){Height=new GridLength(1,GridUnitType.Star)});listPane.RowDefinitions.Add(new(){Height=GridLength.Auto});
        List.ItemsSource=rows;
        List.ItemTemplate=(DataTemplate)XamlReader.Load("""
        <DataTemplate xmlns="http://schemas.microsoft.com/winfx/2006/xaml/presentation"><StackPanel Spacing="4" Padding="4,9,4,9"><TextBlock Text="{Binding Title}" ToolTipService.ToolTip="{Binding Title}" FontSize="16" FontWeight="SemiBold" TextWrapping="Wrap" MaxLines="2"/><TextBlock Text="{Binding Subtitle}" Opacity="0.75" TextWrapping="Wrap"/><TextBlock Text="{Binding Metrics}" TextWrapping="Wrap"/><TextBlock Text="{Binding Evidence}" Opacity="0.75" FontSize="12" TextWrapping="Wrap"/></StackPanel></DataTemplate>
        """);
        List.ItemContainerStyle=new Style(typeof(ListViewItem));List.ItemContainerStyle.Setters.Add(new Setter(Control.HorizontalContentAlignmentProperty,HorizontalAlignment.Stretch));
        AutomationProperties.SetName(List,l.Text("分析结果，选择一项查看详情","Analysis results. Select an item for details"));
        List.SelectionChanged+=(_,_)=>{if(!updating && List.SelectedItem is AnalysisRow row)Selected?.Invoke(row);};listPane.Children.Add(List);
        List.ItemClick+=(_,args)=>{if(!updating && args.ClickedItem is AnalysisRow row)Selected?.Invoke(row);};
        First=new(){Content=l.Text("首页","First")};Previous=new(){Content=l.Text("上一页","Previous")};Next=new(){Content=l.Text("下一页","Next")};
        var pagination=new StackPanel{Orientation=Orientation.Horizontal,Spacing=6};pagination.Children.Add(First);pagination.Children.Add(Previous);pagination.Children.Add(PageLabel);pagination.Children.Add(Next);PageLabel.VerticalAlignment=VerticalAlignment.Center;
        var pagingScroll=new ScrollViewer{Content=pagination,HorizontalScrollBarVisibility=ScrollBarVisibility.Auto,VerticalScrollBarVisibility=ScrollBarVisibility.Disabled};Grid.SetRow(pagingScroll,1);listPane.Children.Add(pagingScroll);
        Back=new(){Content=l.Text("返回列表","Back to list")};Back.Click+=(_,_)=>{ShowDetail(false);List.Focus(FocusState.Programmatic);};
        Detail.Children.Add(Back);Detail.Children.Add(DetailStatus);Detail.Children.Add(DetailBody);
        detailPane=new(){Content=Detail,VerticalScrollBarVisibility=ScrollBarVisibility.Auto,HorizontalScrollBarVisibility=ScrollBarVisibility.Disabled,HorizontalContentAlignment=HorizontalAlignment.Stretch};
        Grid.SetColumn(divider,1);Grid.SetColumn(detailPane,2);split.Children.Add(listPane);split.Children.Add(divider);split.Children.Add(detailPane);Grid.SetRow(split,2);View.Children.Add(split);
        divider.DragDelta+=(_,args)=>{listWidth=Math.Clamp(listWidth+args.HorizontalChange,300,Math.Max(300,split.ActualWidth-400));Layout();};
        AutomationProperties.SetName(divider,l.Text("调整列表与详情宽度，使用左右箭头","Resize list and detail panes using left and right arrows"));
        divider.KeyDown+=(_,args)=>{if(args.Key is Windows.System.VirtualKey.Left or Windows.System.VirtualKey.Right){listWidth=Math.Clamp(listWidth+(args.Key==Windows.System.VirtualKey.Left?-20:20),300,Math.Max(300,split.ActualWidth-400));Layout();args.Handled=true;}};
        split.SizeChanged+=(_,_)=>Layout();
        DetailBody.Children.Add(PresentationControls.Text(l.Text("从列表选择会话或项目查看详情。","Select a session or project from the list.")));
    }
    public void ShowDetail(bool value){selected=value;Layout();if(value && split.ActualWidth<980)Back.Focus(FocusState.Programmatic);}
    private void Layout()
    {
        var narrow=split.ActualWidth<980;
        split.ColumnDefinitions[0].Width=narrow?new GridLength(1,GridUnitType.Star):new GridLength(Math.Clamp(listWidth,300,Math.Max(300,split.ActualWidth-400)));
        split.ColumnDefinitions[1].Width=GridLength.Auto;split.ColumnDefinitions[2].Width=narrow?new GridLength(0):new GridLength(1,GridUnitType.Star);
        listPane.Visibility=narrow && selected?Visibility.Collapsed:Visibility.Visible;
        detailPane.Visibility=narrow && !selected?Visibility.Collapsed:Visibility.Visible;divider.Visibility=narrow?Visibility.Collapsed:Visibility.Visible;
        Back.Visibility=narrow?Visibility.Visible:Visibility.Collapsed;Grid.SetColumn(detailPane,narrow?0:2);
    }
    public void Rows(IEnumerable<AnalysisRow> source,string? selectedId)
    {
        var next=source.ToArray();updating=true;
        if(rows.Select(row=>row.Identity).SequenceEqual(next.Select(row=>row.Identity)))for(var i=0;i<rows.Count;i++)rows[i].Update(next[i]);
        else{rows.Clear();foreach(var row in next)rows.Add(row);}
        List.SelectedItem=rows.FirstOrDefault(row=>row.Identity==selectedId);updating=false;
    }
    public void Paging(CursorPager pager,Codexpulse.Core.V1.PageInfo? info,bool busy)
    {First.IsEnabled=!busy && pager.Cursor is not null;Previous.IsEnabled=!busy && pager.CanPrevious;Next.IsEnabled=!busy && info is{HasMore:true,HasNextCursor:true};PageLabel.Text=pager.Page.ToString();}
    public static ComboBox Combo(string header,IEnumerable<(string Value,string Label)> choices,string selected)
    {
        var combo=new ComboBox{Header=header,MinWidth=160,HorizontalAlignment=HorizontalAlignment.Stretch};
        foreach(var choice in choices)combo.Items.Add(new ComboBoxItem{Tag=choice.Value,Content=choice.Label});
        combo.SelectedItem=combo.Items.Cast<ComboBoxItem>().FirstOrDefault(item=>(string)item.Tag==selected)??combo.Items[0];return combo;
    }
    public static string Choice(ComboBox combo)=>combo.SelectedItem is ComboBoxItem{Tag:string value}?value:"";
    public static void Options(ComboBox combo,IEnumerable<(string Value,string Label)> options,string selected,string all,string selectedFallback)
    {
        if(combo.IsDropDownOpen)return;
        var current=Choice(combo);
        if(combo.Tag is true)selected=current;else combo.Tag=true;
        var selectedName=current==selected?(combo.SelectedItem as ComboBoxItem)?.Content?.ToString():null;
        var values=options.Where(item=>item.Value.Length>0).DistinctBy(item=>item.Value).ToList();
        if(selected.Length>0 && values.All(item=>item.Value!=selected))values.Insert(0,(selected,selectedName??selectedFallback));
        var existing=combo.Items.Cast<ComboBoxItem>().Skip(1).Select(item=>((string)item.Tag,item.Content?.ToString()??"")).ToArray();
        if(existing.SequenceEqual(values) && Choice(combo)==selected)return;
        combo.Items.Clear();combo.Items.Add(new ComboBoxItem{Tag="",Content=all});
        var names=values.GroupBy(item=>item.Label).ToDictionary(group=>group.Key,group=>group.Count());var ordinal=new Dictionary<string,int>();
        foreach(var option in values){ordinal.TryGetValue(option.Label,out var index);ordinal[option.Label]=++index;combo.Items.Add(new ComboBoxItem{Tag=option.Value,Content=option.Label+(names[option.Label]>1?$" ({index})":"")});}
        combo.SelectedItem=combo.Items.Cast<ComboBoxItem>().FirstOrDefault(item=>(string)item.Tag==selected)??combo.Items[0];
    }
}

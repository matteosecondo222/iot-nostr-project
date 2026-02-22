using DotNetEnv;
using System;
using System.Collections.Generic;
using System.Globalization;
using System.IO;
using System.Linq;
using System.Net.WebSockets;
using System.Text;
using System.Text.Json;
using System.Threading;
using System.Threading.Tasks;
using System.Windows.Forms;
using ScottPlot;
using ScottPlot.WinForms;

// --- IMPORT NECESSARI PER LA CRITTOGRAFIA NIP-04 ---
using Org.BouncyCastle.Asn1.X9;
using Org.BouncyCastle.Crypto.Agreement;
using Org.BouncyCastle.Crypto.Parameters;
using Org.BouncyCastle.Security;
using System.Security.Cryptography;

namespace NostrClient
{
    public class SensorData
    {
        public string PubKey { get; set; } = "";
        
        public string SensorId { get; set; } = "In attesa di dati...";
        public string SensorType { get; set; } = "Sconosciuto";
        public int MessageCount { get; set; } = 0;

        public bool IsReceivingHistory { get; set; } = true;
        
        public List<(DateTime Time, string Message)> HistoricalLogs { get; set; } = new();
        
        // MIGLIORIA: Ora DisplayLogs ha il timestamp incorporato per essere ordinato cronologicamente!
        public List<(DateTime Time, string Message)> DisplayLogs { get; set; } = new();
        
        public List<(double Time, double Temp)> DataPoints { get; set; } = new();

        public override string ToString()
        {
            if (SensorId == "In attesa di dati...")
                return $"[{PubKey.Substring(0, 8)}...] In attesa...";
            
            return SensorId; 
        }
    }

    public partial class Form1 : Form
    {
        private readonly string dashboardPrivKeyHex; 

        private readonly string[] relayUrls = { "wss://relay.damus.io", "ws://localhost:3334" };
        
        private List<ClientWebSocket> activeWebSockets = new List<ClientWebSocket>();
        private HashSet<string> processedEventIds = new HashSet<string>();

        // UI Controls
        private TextBox? txtLog;
        private FormsPlot? formsPlot;
        private CheckedListBox? chkSensors;
        private TextBox? txtNewSensorKey;
        private Button? btnAddSensor;
        
        private Panel? pnlLogHeader;
        private ComboBox? cmbLogSelector;

        private Dictionary<string, SensorData> sensors = new Dictionary<string, SensorData>();
        private string currentSelectedPubKey = "";

        public Form1()
        {
            Env.TraversePath().Load();
            dashboardPrivKeyHex = Env.GetString("DASHBOARD_PRIV_KEY");

            if (string.IsNullOrEmpty(dashboardPrivKeyHex))
            {
                MessageBox.Show("Errore Critico: Impossibile trovare DASHBOARD_PRIV_KEY nel file .env!", "Sicurezza", MessageBoxButtons.OK, MessageBoxIcon.Error);
                Environment.Exit(1);
            }

            SetupUI();
            
            this.Load += async (s, e) => await ConnectAndListenAsync();
            this.FormClosing += async (s, e) => await DisconnectAsync();
        }

        private void SetupUI()
        {
            this.Text = "Hub Sensori IoT (Multi-Relay Criptato NIP-04)";
            this.Width = 1000;
            this.Height = 800; 

            SplitContainer mainSplit = new SplitContainer
            {
                Dock = DockStyle.Fill,
                Orientation = System.Windows.Forms.Orientation.Horizontal,
                SplitterDistance = 450 
            };

            formsPlot = new FormsPlot { Dock = DockStyle.Fill };
            formsPlot.Plot.Title("Confronto Sensori IoT (Dati Decriptati)");
            formsPlot.Plot.Axes.Left.Label.Text = "Temperatura (°C)";
            formsPlot.Plot.Axes.Bottom.Label.Text = "Orario";
            formsPlot.Plot.Axes.DateTimeTicksBottom();
            
            mainSplit.Panel1.Controls.Add(formsPlot);

            SplitContainer bottomSplit = new SplitContainer
            {
                Dock = DockStyle.Fill,
                Orientation = System.Windows.Forms.Orientation.Vertical,
                SplitterDistance = 650 
            };

            pnlLogHeader = new Panel { Dock = DockStyle.Top, Height = 30, BackColor = System.Drawing.Color.FromArgb(40, 40, 40), Visible = false };
            System.Windows.Forms.Label lblSelectLog = new System.Windows.Forms.Label { Text = "Mostra log del sensore:", Dock = DockStyle.Left, Width = 150, TextAlign = System.Drawing.ContentAlignment.MiddleLeft, ForeColor = System.Drawing.Color.White };
            cmbLogSelector = new ComboBox { Dock = DockStyle.Fill, DropDownStyle = ComboBoxStyle.DropDownList };
            cmbLogSelector.SelectedIndexChanged += CmbLogSelector_SelectedIndexChanged;
            
            pnlLogHeader.Controls.Add(cmbLogSelector);
            pnlLogHeader.Controls.Add(lblSelectLog);

            txtLog = new TextBox
            {
                Multiline = true,
                Dock = DockStyle.Fill,
                ReadOnly = true,
                ScrollBars = ScrollBars.Vertical,
                BackColor = System.Drawing.Color.Black,
                ForeColor = System.Drawing.Color.Lime,
                Font = new System.Drawing.Font("Consolas", 10F)
            };
            
            bottomSplit.Panel1.Controls.Add(txtLog);
            bottomSplit.Panel1.Controls.Add(pnlLogHeader);
            txtLog.BringToFront();

            Panel rightPanel = new Panel { Dock = DockStyle.Fill, Padding = new Padding(10) };
            
            System.Windows.Forms.Label lblInput = new System.Windows.Forms.Label { Text = "Aggiungi PubKey Sensore:", Dock = DockStyle.Top, Height = 25 };
            txtNewSensorKey = new TextBox { Dock = DockStyle.Top, Height = 30 };
            
            btnAddSensor = new Button { Text = "Aggiungi e Monitora", Dock = DockStyle.Top, Height = 40, Margin = new Padding(0, 5, 0, 15) };
            btnAddSensor.Click += BtnAddSensor_Click;

            System.Windows.Forms.Label lblList = new System.Windows.Forms.Label { Text = "Sensori Monitorati:", Dock = DockStyle.Top, Height = 25 };
            
            chkSensors = new CheckedListBox 
            { 
                Dock = DockStyle.Fill, 
                CheckOnClick = true 
            };

            Button btnShowDetails = new Button { 
                Text = "Mostra Dettagli Sensori", 
                Dock = DockStyle.Bottom, 
                Height = 40, 
                Margin = new Padding(0, 10, 0, 0),
                BackColor = System.Drawing.Color.LightBlue 
            };
            btnShowDetails.Click += BtnShowDetails_Click;

            chkSensors.SelectedIndexChanged += ChkSensors_SelectedIndexChanged;
            chkSensors.ItemCheck += ChkSensors_ItemCheck;

            rightPanel.Controls.Add(chkSensors);
            rightPanel.Controls.Add(lblList);
            rightPanel.Controls.Add(btnAddSensor);
            rightPanel.Controls.Add(txtNewSensorKey);
            rightPanel.Controls.Add(lblInput);
            rightPanel.Controls.Add(btnShowDetails);

            bottomSplit.Panel2.Controls.Add(rightPanel);
            mainSplit.Panel2.Controls.Add(bottomSplit);

            this.Controls.Add(mainSplit);
        }

        private async void BtnAddSensor_Click(object? sender, EventArgs e)
        {
            string newKey = txtNewSensorKey?.Text.Trim() ?? "";
            
            if (newKey.Length != 64)
            {
                MessageBox.Show("La PubKey deve essere di 64 caratteri esadecimali.", "Errore", MessageBoxButtons.OK, MessageBoxIcon.Warning);
                return;
            }

            if (sensors.ContainsKey(newKey))
            {
                MessageBox.Show("Sensore già monitorato!", "Info", MessageBoxButtons.OK, MessageBoxIcon.Information);
                return;
            }

            sensors[newKey] = new SensorData { PubKey = newKey };
            chkSensors?.Items.Add(sensors[newKey]);
            txtNewSensorKey!.Text = "";

            int newIndex = chkSensors!.Items.Count - 1;
            chkSensors.SelectedIndex = newIndex;
            chkSensors.SetItemChecked(newIndex, true);
            currentSelectedPubKey = newKey; 

            string reqMessage = $@"[""REQ"", ""sub-{newKey}"", {{""authors"": [""{newKey}""], ""kinds"": [4]}}]";
            var bytes = Encoding.UTF8.GetBytes(reqMessage);

            foreach (var ws in activeWebSockets)
            {
                if (ws.State == WebSocketState.Open)
                {
                    await ws.SendAsync(new ArraySegment<byte>(bytes), WebSocketMessageType.Text, true, CancellationToken.None);
                }
            }
            
            // AGGIORNATO: Aggiungiamo il timestamp fittizio per il log di sistema
            sensors[newKey].DisplayLogs.Add((DateTime.Now, $"[SISTEMA] Sottoscrizione inviata per il sensore {newKey.Substring(0,8)} su tutti i relay..."));
            RefreshUI();
        }

        private void BtnShowDetails_Click(object? sender, EventArgs e)
        {
            Form detailsForm = new Form
            {
                Text = "Dettagli Sensori Monitorati",
                Width = 800,
                Height = 400,
                StartPosition = FormStartPosition.CenterParent
            };

            DataGridView grid = new DataGridView
            {
                Dock = DockStyle.Fill,
                AutoSizeColumnsMode = DataGridViewAutoSizeColumnsMode.Fill,
                AllowUserToAddRows = false,
                ReadOnly = true,
                RowHeadersVisible = false,
                SelectionMode = DataGridViewSelectionMode.FullRowSelect
            };

            grid.Columns.Add("ID", "Sensor ID");
            grid.Columns.Add("Type", "Tipo (Tag 't')");
            grid.Columns.Add("Messages", "Messaggi Ricevuti");
            grid.Columns.Add("PubKey", "Chiave Pubblica");

            foreach (var kvp in sensors)
            {
                var s = kvp.Value;
                grid.Rows.Add(s.SensorId, s.SensorType, s.MessageCount, s.PubKey);
            }

            detailsForm.Controls.Add(grid);
            detailsForm.ShowDialog();
        }

        private void CmbLogSelector_SelectedIndexChanged(object? sender, EventArgs e)
        {
            if (cmbLogSelector?.SelectedItem is SensorData selectedSensor)
            {
                currentSelectedPubKey = selectedSensor.PubKey;
                if (chkSensors != null)
                {
                    int index = chkSensors.Items.IndexOf(selectedSensor);
                    if (index >= 0 && chkSensors.SelectedIndex != index)
                    {
                        chkSensors.SelectedIndex = index;
                    }
                }
                RefreshUI();
            }
        }

        private void ChkSensors_SelectedIndexChanged(object? sender, EventArgs e)
        {
            if (chkSensors?.SelectedItem is SensorData selectedSensor)
            {
                if (chkSensors.CheckedItems.Contains(selectedSensor))
                {
                    currentSelectedPubKey = selectedSensor.PubKey;
                    if (cmbLogSelector != null && cmbLogSelector.SelectedItem != selectedSensor)
                    {
                        cmbLogSelector.SelectedItem = selectedSensor;
                    }
                    RefreshUI();
                }
            }
        }

        private void ChkSensors_ItemCheck(object? sender, ItemCheckEventArgs e)
        {
            BeginInvoke(new Action(() => 
            {
                if (e.NewValue == CheckState.Unchecked && chkSensors?.SelectedIndex == e.Index)
                {
                    chkSensors.ClearSelected();
                }
                else if (e.NewValue == CheckState.Checked && chkSensors != null)
                {
                    chkSensors.SelectedIndex = e.Index;
                }

                UpdateLogSelectorBar();
                RefreshUI();
            }));
        }
    
        private void UpdateLogSelectorBar()
        {
            if (cmbLogSelector == null || chkSensors == null || pnlLogHeader == null) return;

            string prevPubKey = currentSelectedPubKey;

            cmbLogSelector.Items.Clear();
            foreach (SensorData item in chkSensors.CheckedItems)
            {
                cmbLogSelector.Items.Add(item);
            }

            if (cmbLogSelector.Items.Count > 0)
            {
                pnlLogHeader.Visible = true; 
                var itemToSelect = cmbLogSelector.Items.Cast<SensorData>().FirstOrDefault(s => s.PubKey == prevPubKey);
                if (itemToSelect != null)
                {
                    cmbLogSelector.SelectedItem = itemToSelect;
                }
                else
                {
                    cmbLogSelector.SelectedIndex = 0;
                }
            }
            else
            {
                pnlLogHeader.Visible = false;
                currentSelectedPubKey = "";
            }
        }

        private void RefreshUI()
        {
            if (formsPlot != null && chkSensors != null)
            {
                formsPlot.Plot.Clear();
                formsPlot.Plot.Title("Confronto Sensori IoT (Dati Decriptati)");

                var palette = new[] { Colors.Red, Colors.Blue, Colors.Green, Colors.Orange, Colors.Purple, Colors.Teal };
                int colorIndex = 0;

                foreach (object item in chkSensors.CheckedItems)
                {
                    if (item is SensorData sensor && sensor.DataPoints.Count > 0)
                    {
                        double[] sortedTimes = new double[sensor.DataPoints.Count];
                        double[] sortedTemps = new double[sensor.DataPoints.Count];
                        for (int i = 0; i < sensor.DataPoints.Count; i++)
                        {
                            sortedTimes[i] = sensor.DataPoints[i].Time;
                            sortedTemps[i] = sensor.DataPoints[i].Temp;
                        }

                        var scatter = formsPlot.Plot.Add.Scatter(sortedTimes, sortedTemps);
                        scatter.Color = palette[colorIndex % palette.Length];
                        scatter.LineWidth = 2;
                        scatter.Label = sensor.SensorId; 
                        
                        colorIndex++;
                    }
                }

                formsPlot.Plot.ShowLegend();
                formsPlot.Plot.Axes.DateTimeTicksBottom();
                formsPlot.Plot.Axes.AutoScale();
                formsPlot.Refresh();
            }

            if (txtLog != null && chkSensors != null)
            {
                if (chkSensors.CheckedItems.Count == 0)
                {
                    txtLog.Text = "Nessun sensore spuntato. Accendi un sensore dalla lista per visualizzarne i log e il grafico.";
                }
                else if (!string.IsNullOrEmpty(currentSelectedPubKey) && sensors.ContainsKey(currentSelectedPubKey))
                {
                    var activeSensor = sensors[currentSelectedPubKey];
                    // AGGIORNATO: Estraiamo solo i testi dal nostro nuovo DisplayLogs formattato a tuple
                    txtLog.Text = string.Join(Environment.NewLine, activeSensor.DisplayLogs.Select(x => x.Message)) + Environment.NewLine;
                    txtLog.SelectionStart = txtLog.Text.Length;
                    txtLog.ScrollToCaret();
                }
            }
        }

        private async Task ConnectAndListenAsync()
        {
            var tasks = relayUrls.Select(url => ConnectToSingleRelayAsync(url)).ToArray();
            await Task.WhenAll(tasks);
        }

        private async Task ConnectToSingleRelayAsync(string url)
        {
            ClientWebSocket ws = new ClientWebSocket();
            activeWebSockets.Add(ws);

            try
            {
                await ws.ConnectAsync(new Uri(url), CancellationToken.None);
                
                foreach (var pubKey in sensors.Keys)
                {
                    string reqMessage = $@"[""REQ"", ""sub-{pubKey}"", {{""authors"": [""{pubKey}""], ""kinds"": [4]}}]";
                    var bytes = Encoding.UTF8.GetBytes(reqMessage);
                    await ws.SendAsync(new ArraySegment<byte>(bytes), WebSocketMessageType.Text, true, CancellationToken.None);
                }

                var buffer = new byte[8192]; 
                while (ws.State == WebSocketState.Open)
                {
                    using var ms = new MemoryStream();
                    WebSocketReceiveResult result;
                    do
                    {
                        result = await ws.ReceiveAsync(new ArraySegment<byte>(buffer), CancellationToken.None);
                        ms.Write(buffer, 0, result.Count);
                    } while (!result.EndOfMessage);

                    if (result.MessageType == WebSocketMessageType.Close) break;

                    var jsonResponse = Encoding.UTF8.GetString(ms.ToArray());
                    
                    // AGGIORNATO: Passiamo l'URL alla funzione per identificare il relay!
                    ProcessNostrMessage(jsonResponse, url);
                }
            }
            catch (Exception ex)
            {
                Invoke(new Action(() => 
                {
                    if (txtLog != null) txtLog.AppendText($"\n[SISTEMA] Connessione a {url} caduta: {ex.Message}\n");
                }));
            }
        }

        private string DecryptNip04(string encryptedPayload, string senderPubKeyHex)
        {
            try
            {
                var curve = ECNamedCurveTable.GetByName("secp256k1");
                var domainParams = new ECDomainParameters(curve.Curve, curve.G, curve.N, curve.H);

                var privKeyInt = new Org.BouncyCastle.Math.BigInteger(dashboardPrivKeyHex, 16);
                var privKeyParam = new ECPrivateKeyParameters(privKeyInt, domainParams);

                var pubKeyPoint = curve.Curve.DecodePoint(Convert.FromHexString("02" + senderPubKeyHex));
                var pubKeyParam = new ECPublicKeyParameters(pubKeyPoint, domainParams);

                var ecdh = new ECDHBasicAgreement();
                ecdh.Init(privKeyParam);
                var sharedSecret = ecdh.CalculateAgreement(pubKeyParam).ToByteArrayUnsigned();

                if (sharedSecret.Length < 32)
                {
                    byte[] padded = new byte[32];
                    Array.Copy(sharedSecret, 0, padded, 32 - sharedSecret.Length, sharedSecret.Length);
                    sharedSecret = padded;
                }

                var parts = encryptedPayload.Split("?iv=");
                if (parts.Length != 2) return "";

                byte[] cipherText = Convert.FromBase64String(parts[0]);
                byte[] iv = Convert.FromBase64String(parts[1]);

                using Aes aes = Aes.Create();
                aes.KeySize = 256;
                aes.Key = sharedSecret;
                aes.IV = iv;
                aes.Mode = CipherMode.CBC;
                aes.Padding = PaddingMode.PKCS7;

                using ICryptoTransform decryptor = aes.CreateDecryptor(aes.Key, aes.IV);
                using MemoryStream ms = new MemoryStream(cipherText);
                using CryptoStream cs = new CryptoStream(ms, decryptor, CryptoStreamMode.Read);
                using StreamReader sr = new StreamReader(cs);
                
                return sr.ReadToEnd();
            }
            catch
            {
                return "[Errore di Decrittazione]";
            }
        }

        // AGGIORNATO: Ora riceve string relayUrl come parametro
        private void ProcessNostrMessage(string json, string relayUrl)
        {
            try
            {
                // Estraiamo il nome del Relay (es. "relay.damus.io") dall'URL
                string relayName = new Uri(relayUrl).Host;

                using var doc = JsonDocument.Parse(json);
                var root = doc.RootElement;

                if (root.ValueKind == JsonValueKind.Array && root.GetArrayLength() >= 2)
                {
                    var messageType = root[0].GetString();

                    if (messageType == "EVENT" && root.GetArrayLength() >= 3)
                    {
                        var nostrEvent = root[2];

                        var eventId = nostrEvent.GetProperty("id").GetString();
                        if (!string.IsNullOrEmpty(eventId))
                        {
                            if (processedEventIds.Contains(eventId)) return;
                            processedEventIds.Add(eventId);
                        }

                        var pubKey = nostrEvent.GetProperty("pubkey").GetString();
                        if (string.IsNullOrEmpty(pubKey) || !sensors.ContainsKey(pubKey)) return;

                        var sensor = sensors[pubKey];
                        
                        var rawContent = nostrEvent.GetProperty("content").GetString() ?? "";
                        var content = DecryptNip04(rawContent, pubKey);

                        if (content.StartsWith("[Errore") || string.IsNullOrEmpty(content)) return;

                        var createdAtUnix = nostrEvent.GetProperty("created_at").GetInt64();
                        var timestamp = DateTimeOffset.FromUnixTimeSeconds(createdAtUnix).ToLocalTime();

                        sensor.MessageCount++; 

                        bool needsListRefresh = false;
                        var tagsArray = nostrEvent.GetProperty("tags");
                        foreach (var tag in tagsArray.EnumerateArray())
                        {
                            if (tag.GetArrayLength() >= 2)
                            {
                                string tagName = tag[0].GetString() ?? "";
                                string tagValue = tag[1].GetString() ?? "";

                                if (tagName == "sensor_id" && sensor.SensorId != tagValue)
                                {
                                    sensor.SensorId = tagValue;
                                    needsListRefresh = true; 
                                }
                                if (tagName == "t" && sensor.SensorType != tagValue)
                                {
                                    sensor.SensorType = tagValue;
                                }
                            }
                        }

                        if (needsListRefresh)
                        {
                            Invoke((MethodInvoker)delegate {
                                int index = chkSensors!.Items.IndexOf(sensor);
                                if (index >= 0)
                                {
                                    bool wasSelected = chkSensors.SelectedIndex == index;
                                    chkSensors.Items[index] = chkSensors.Items[index]; 
                                    if (wasSelected) chkSensors.SelectedIndex = index;
                                }
                                
                                if (cmbLogSelector != null)
                                {
                                    int cmbIndex = cmbLogSelector.Items.IndexOf(sensor);
                                    if (cmbIndex >= 0)
                                    {
                                        cmbLogSelector.Items[cmbIndex] = cmbLogSelector.Items[cmbIndex];
                                    }
                                }
                            });
                        }

                        // AGGIORNATO: Aggiungiamo [RelayName] all'inizio del log testuale!
                        string logLine = $"[{relayName}] [{timestamp:dd/MM/yyyy HH:mm:ss}] 🔒 {content}°C";

                        if (sensor.IsReceivingHistory)
                        {
                            sensor.HistoricalLogs.Add((timestamp.LocalDateTime, logLine));
                        }
                        else
                        {
                            // Aggiungiamo il nuovo messaggio e RIO-ORDINIAMO in tempo reale per timestamp
                            sensor.DisplayLogs.Add((timestamp.LocalDateTime, logLine));
                            sensor.DisplayLogs.Sort((a, b) => a.Time.CompareTo(b.Time));
                            
                            if (sensor.DisplayLogs.Count > 100) sensor.DisplayLogs.RemoveAt(0);

                            Invoke(new Action(() => 
                            {
                                bool isChecked = chkSensors != null && chkSensors.CheckedItems.Contains(sensor);
                                bool isSelected = currentSelectedPubKey == pubKey; 
                                if (isChecked || isSelected)
                                {
                                    RefreshUI();
                                }
                            }));
                        }

                        if (!string.IsNullOrEmpty(content))
                        {
                            string tempStr = content.Replace("Lettura Sensore: ", "").Replace(" °C", "").Trim();
                            if (double.TryParse(tempStr, NumberStyles.Any, CultureInfo.InvariantCulture, out double tempValue))
                            {
                                sensor.DataPoints.Add((timestamp.LocalDateTime.ToOADate(), tempValue));
                                if (sensor.DataPoints.Count > 500) sensor.DataPoints.RemoveAt(0);
                                sensor.DataPoints.Sort((a, b) => a.Time.CompareTo(b.Time));
                            }
                        }
                    }
                    else if (messageType == "EOSE")
                    {
                        var subId = root[1].GetString();
                        if (subId != null && subId.StartsWith("sub-"))
                        {
                            var pubKey = subId.Replace("sub-", "");
                            if (sensors.ContainsKey(pubKey))
                            {
                                var sensor = sensors[pubKey];
                                sensor.IsReceivingHistory = false;

                                // Trasferiamo i log storici nel contenitore visuale
                                foreach (var log in sensor.HistoricalLogs)
                                {
                                    sensor.DisplayLogs.Add(log);
                                }
                                sensor.HistoricalLogs.Clear();
                                
                                // AGGIORNATO: Inseriamo il messaggio di sistema differenziato per relay
                                sensor.DisplayLogs.Add((DateTime.Now, $"[SISTEMA] --- Storico da [{relayName}] completato ---"));

                                // RIO-ORDINIAMO TUTTO il blocco di messaggi in base all'orario in cui sono successi
                                sensor.DisplayLogs.Sort((a, b) => a.Time.CompareTo(b.Time));

                                while (sensor.DisplayLogs.Count > 100)
                                {
                                    sensor.DisplayLogs.RemoveAt(0);
                                }

                                Invoke(new Action(() => 
                                {
                                    bool isChecked = chkSensors != null && chkSensors.CheckedItems.Contains(sensor);
                                    bool isSelected = currentSelectedPubKey == pubKey;
                                    if (isChecked || isSelected)
                                    {
                                        RefreshUI();
                                    }
                                }));
                            }
                        }
                    }
                }
            }
            catch
            {
                // Ignora pacchetti malformati
            }
        }

        private async Task DisconnectAsync()
        {
            foreach(var ws in activeWebSockets)
            {
                if (ws.State == WebSocketState.Open)
                {
                    await ws.CloseAsync(WebSocketCloseStatus.NormalClosure, "Chiusura app", CancellationToken.None);
                }
            }
        }
    }
}